package audit

import (
	"fmt"
	"sort"
	"time"

	"github.com/mayagurutech/tallyguru/internal/mapping"
	"github.com/mayagurutech/tallyguru/internal/sections"
)

type dayKey struct{ date, ledger string }

type dayAgg struct {
	amount   int64
	vouchers []string
}

type tdsAgg struct {
	tag, expense  string
	total, maxOne int64
	month         map[string]int64
	vouchers      []string
	direct        bool // paid from bank/cash with no party ledger credited
	tdsInEntries  bool // a TDS payable line appears in one of these entries
}

type dupKey struct {
	typ, party string
	value      int64
}

type dupSeen struct{ date, guid, text string } // text: narration or reference

// state collects every per-voucher check in one pass.
type state struct {
	ls                                map[string]*ledger
	cashPay, cashRec, loanIn, loanOut map[dayKey]*dayAgg
	tds                               map[[2]string]*tdsAgg // party, expense ledger
	tdsSeen                           map[string]bool       // parties with TDS in any voucher
	related                           map[string]*Issue
	dups                              map[dupKey][]dupSeen
	numbers                           map[[2]string][]string // type+number -> guids
	round, sunday                     []Issue
	companyState                      string
	gstState                          []Issue
	gstNoState, gstNoParty            int
	foreignAgg                        map[string]*Issue
}

func newState(ls map[string]*ledger) *state {
	return &state{ls: ls, cashPay: map[dayKey]*dayAgg{}, cashRec: map[dayKey]*dayAgg{}, loanIn: map[dayKey]*dayAgg{},
		loanOut: map[dayKey]*dayAgg{}, tds: map[[2]string]*tdsAgg{}, tdsSeen: map[string]bool{}, related: map[string]*Issue{},
		dups: map[dupKey][]dupSeen{}, numbers: map[[2]string][]string{}, foreignAgg: map[string]*Issue{}}
}

func (s *state) led(n string) *ledger {
	if l := s.ls[n]; l != nil {
		return l
	}
	return &ledger{name: n}
}

func addDay(m map[dayKey]*dayAgg, date, ledger, guid string, amt int64) {
	k := dayKey{date, ledger}
	a := m[k]
	if a == nil {
		a = &dayAgg{}
		m[k] = a
	}
	a.amount += amt
	a.vouchers = append(a.vouchers, guid)
}

// counterparty is who the voucher is with. Tally's party field holds the bank or cash
// ledger in payments and receipts (seen on the pilot), so then the largest other ledger is used.
func (s *state) counterparty(v voucher) string {
	if p := s.led(v.party); v.party != "" && !p.cash && !p.bank {
		return v.party
	}
	best, amt := "", int64(0)
	for _, l := range v.lines {
		if x := s.led(l.ledger); !x.cash && !x.bank && abs(l.amount) > amt {
			best, amt = l.ledger, abs(l.amount)
		}
	}
	if best == "" {
		return v.party // bank-to-cash transfers: keep what Tally shows
	}
	return best
}

func (s *state) add(v voucher) {
	v.party = s.counterparty(v)
	s.checkGSTState(v, s.led(v.party))
	for _, l := range v.lines {
		if x := s.led(l.ledger); x.foreign && l.amount != 0 {
			f := s.foreignAgg[x.name]
			if f == nil {
				f = &Issue{Party: x.name}
				s.foreignAgg[x.name] = f
			}
			f.Amount += abs(l.amount)
			f.Vouchers = append(f.Vouchers, v.guid)
			f.Detail = fmt.Sprintf("%d entries, country %s (debits and credits together)", len(f.Vouchers), x.country)
		}
	}
	var cashCr, cashDr, otherCr, otherDr int64
	var creditors, credited []string
	hasTDS := false
	for _, l := range v.lines {
		x := s.led(l.ledger)
		switch {
		case x.cash && l.amount > 0:
			cashCr += l.amount
		case x.cash:
			cashDr += -l.amount
		case l.amount > 0:
			otherCr += l.amount
		default:
			otherDr += -l.amount
		}
		if x.creditor {
			creditors = append(creditors, x.name)
			if l.amount > 0 {
				credited = append(credited, x.name)
			}
		}
		if x.tdsPayable && l.amount != 0 {
			hasTDS = true
		}
		if x.related && l.amount != 0 {
			r := s.related[x.name]
			if r == nil {
				r = &Issue{Party: x.name}
				s.related[x.name] = r
			}
			r.Amount += abs(l.amount)
			r.Vouchers = append(r.Vouchers, v.guid)
		}
	}
	if hasTDS {
		for _, c := range creditors {
			s.tdsSeen[c] = true
		}
		if v.party != "" {
			s.tdsSeen[v.party] = true
		}
	}

	// Cash paid out: each non-cash debit's share of the cash part of the voucher.
	if cashCr > 0 && otherDr > 0 {
		for _, l := range v.lines {
			x := s.led(l.ledger)
			if l.amount >= 0 || x.cash || x.bank || x.capital || x.tax {
				continue
			}
			share := -l.amount * cashCr / max(otherDr, cashCr)
			if x.loan {
				addDay(s.loanOut, v.date, x.name, v.guid, share)
				continue
			}
			addDay(s.cashPay, v.date, x.name, v.guid, share)
		}
	}
	// Cash received: each non-cash credit's share of the cash part.
	if cashDr > 0 && otherCr > 0 {
		for _, l := range v.lines {
			x := s.led(l.ledger)
			if l.amount <= 0 || x.cash || x.bank {
				continue
			}
			share := l.amount * cashDr / max(otherCr, cashDr)
			if x.loan { // loans/deposits have their own check (269SS), not 269ST
				addDay(s.loanIn, v.date, x.name, v.guid, share)
				continue
			}
			addDay(s.cashRec, v.date, x.name, v.guid, share)
		}
	}

	// TDS: expense debits on ledgers with a TDS section, by party (creditor in the voucher,
	// else the voucher's party, else the expense ledger itself when paid directly).
	for _, l := range v.lines {
		x := s.led(l.ledger)
		if l.amount >= 0 || x.tdsTag == "" || x.tdsTag == "none" {
			continue
		}
		// The party is a creditor CREDITED in this entry (expense booked against it). Tally's
		// party field is not used: in a bank payment covering several items it holds just the
		// first ledger (pilot: rent paid from bank was put on a tea stall). Paid directly from
		// bank or cash: grouped under the expense ledger, for the auditor to see who was paid.
		party := x.name
		if len(credited) == 1 {
			party = credited[0]
		}
		if s.led(party).foreign {
			continue // non-resident: 194C etc. do not apply; listed under foreign parties (pilot Q4)
		}
		k := [2]string{party, x.name}
		a := s.tds[k]
		if a == nil {
			a = &tdsAgg{tag: x.tdsTag, expense: x.name, month: map[string]int64{}}
			s.tds[k] = a
		}
		a.total += -l.amount
		a.maxOne = max(a.maxOne, -l.amount)
		a.month[v.date[:7]] += -l.amount
		a.vouchers = append(a.vouchers, v.guid)
		a.direct = party == x.name
		a.tdsInEntries = a.tdsInEntries || hasTDS
	}

	if v.party != "" && v.value > 0 {
		k := dupKey{v.typ, v.party, v.value}
		// Reference (bill/invoice no.) only: narrations are often generic text repeated on
		// many entries (seen on the pilot: ~380 false pairs).
		text := v.reference
		s.dups[k] = append(s.dups[k], dupSeen{v.date, v.guid, text})
	}
	if v.number != "" {
		k := [2]string{v.typ, v.number}
		s.numbers[k] = append(s.numbers[k], v.guid)
	}
	if (v.base == "Journal" || v.base == "Payment") && v.value >= Limits.RoundMin && v.value%Limits.RoundMultiple == 0 {
		s.round = append(s.round, Issue{Date: ddmmyyyy(v.date), Party: v.party, Amount: v.value,
			Detail: v.typ + " " + v.number, Vouchers: []string{v.guid}})
	}
	if t, err := time.Parse(time.DateOnly, v.date); err == nil && t.Weekday() == time.Sunday {
		s.sunday = append(s.sunday, Issue{Date: ddmmyyyy(v.date), Party: v.party, Amount: v.value,
			Detail: v.typ + " " + v.number, Vouchers: []string{v.guid}})
	}
}

func dayIssues(m map[dayKey]*dayAgg, over func(int64) bool, detail string) []Issue {
	return dayIssuesBy(m, func(_ string, a int64) bool { return over(a) }, detail)
}

func dayIssuesBy(m map[dayKey]*dayAgg, over func(ledger string, amt int64) bool, detail string) []Issue {
	var out []Issue
	for k, a := range m {
		if over(k.ledger, a.amount) {
			d := detail
			if len(a.vouchers) > 1 {
				d += fmt.Sprintf(" (%d entries that day)", len(a.vouchers))
			}
			out = append(out, Issue{Date: ddmmyyyy(k.date), Party: k.ledger, Amount: a.amount, Detail: d, Vouchers: a.vouchers})
		}
	}
	return out
}

func (s *state) finish(rs map[string]*Result) {
	// Transporters (Tally's transporter flag on the party): the higher limit, s. 36(6).
	rs["cash_payments"].Issues = dayIssuesBy(s.cashPay, func(led string, a int64) bool {
		if s.led(led).transporter {
			return a > Limits.CashTransporter
		}
		return a > Limits.CashPayment
	}, "cash paid")
	rs["cash_payments"].Note = "Grouped by ledger and day; where an expense ledger is shown, check who was paid. Parties flagged as transporters in Tally use the 35,000 limit (" +
		sections.Cite("40A(3)-transport") + "). Exceptions (e.g. payments to government, banks) are for the auditor."
	s.finishGST(rs)
	rs["cash_receipts"].Issues = dayIssues(s.cashRec, func(a int64) bool { return a >= Limits.CashReceipt }, "cash received")
	rs["cash_loans_taken"].Issues = dayIssues(s.loanIn, func(a int64) bool { return a >= Limits.CashLoan }, "loan/deposit taken in cash")
	rs["cash_loans_repaid"].Issues = dayIssues(s.loanOut, func(a int64) bool { return a >= Limits.CashLoan }, "loan/deposit repaid in cash")

	for k, a := range s.tds {
		if s.tdsSeen[k[0]] {
			continue
		}
		due := false
		// Thresholds are in rupees; amounts here are paise.
		if t, ok := mapping.Thresholds[a.tag]; ok && t.Monthly > 0 {
			for _, m := range a.month {
				due = due || mapping.Due(a.tag, m/100, 0)
			}
		} else {
			due = mapping.Due(a.tag, a.maxOne/100, (a.total-a.maxOne)/100)
		}
		if due {
			rs["tds_not_deducted"].Issues = append(rs["tds_not_deducted"].Issues, Issue{Party: k[0], Amount: a.total,
				Detail: tdsDetail(a), Vouchers: a.vouchers})
		}
	}

	for _, r := range s.related {
		r.Detail = fmt.Sprintf("%d entries (debits and credits together)", len(r.Vouchers))
		rs["related_parties"].Issues = append(rs["related_parties"].Issues, *r)
	}
	if len(s.related) == 0 {
		rs["related_parties"].Note = "No ledger is tagged 'related party' in the ledger mapping; tag them to use this check."
	}

	for k, seen := range s.dups {
		if len(seen) < 2 {
			continue
		}
		sort.Slice(seen, func(i, j int) bool { return seen[i].date < seen[j].date })
		for i := 1; i < len(seen); i++ {
			a, _ := time.Parse(time.DateOnly, seen[i-1].date)
			b, _ := time.Parse(time.DateOnly, seen[i].date)
			days := int(b.Sub(a).Hours() / 24)
			if days > Limits.DuplicateDays {
				continue
			}
			// Small repeating amounts (staff expenses, fuel) are normal: below the minimum a
			// pair counts only when the narration or reference is also the same.
			same := seen[i].text != "" && seen[i].text == seen[i-1].text
			if k.value < Limits.DuplicateMin && !same {
				continue
			}
			why := "same type (%s), party and amount %d day(s) after %s"
			if same {
				why = "same type (%s), party, amount and bill reference %d day(s) after %s"
			}
			rs["duplicates"].Issues = append(rs["duplicates"].Issues, Issue{Date: ddmmyyyy(seen[i].date), Party: k.party, Amount: k.value,
				Detail:   fmt.Sprintf(why, k.typ, days, ddmmyyyy(seen[i-1].date)),
				Vouchers: []string{seen[i-1].guid, seen[i].guid}})
		}
	}
	for k, gs := range s.numbers {
		if len(gs) > 1 {
			rs["duplicates"].Issues = append(rs["duplicates"].Issues, Issue{Party: "", Amount: 0,
				Detail: fmt.Sprintf("voucher number %q used %d times in %s", k[1], len(gs), k[0]), Vouchers: gs})
		}
	}
	rs["duplicates"].Note = fmt.Sprintf("Same type, party and amount within %d days, where the amount is %s or more or the bill reference is also the same; or a voucher number used twice. Recurring entries will show; judge each.",
		Limits.DuplicateDays, rupees(Limits.DuplicateMin))
	rs["round_sums"].Issues = s.round
	rs["round_sums"].Note = fmt.Sprintf("Journals and payments in exact multiples of %s, of %s or more.", rupees(Limits.RoundMultiple), rupees(Limits.RoundMin))
	rs["sunday_entries"].Issues = s.sunday
}

// tdsDetail says what was found. Paid directly (no party ledger) stays listed: the payee and
// the TDS should sit in the party's ledger (author, 16 Nov 2026).
func tdsDetail(a *tdsAgg) string {
	switch {
	case a.direct && a.tdsInEntries:
		return fmt.Sprintf("%s via %s; paid directly from bank/cash with no party ledger. TDS is in these entries but not against a party: check each payee", a.tag, a.expense)
	case a.direct:
		return fmt.Sprintf("%s via %s; paid directly from bank/cash with no party ledger and no TDS: check each payee and the limit per payee", a.tag, a.expense)
	}
	return fmt.Sprintf("%s via %s; no TDS line in any entry with this party", a.tag, a.expense)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
