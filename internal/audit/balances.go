package audit

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mayagurutech/tallyguru/internal/report"
)

// negativeCash walks each cash ledger day by day (opening + every entry) and lists the days
// that ended with a credit (negative) balance in the period.
func negativeCash(db *sql.DB, ls map[string]*ledger, p report.Period, r *Result) error {
	end, _ := time.Parse("02-01-2006", p.End())
	for _, l := range ls {
		if !l.cash {
			continue
		}
		rows, err := db.Query(`SELECT v.date, SUM(vl.amount) FROM vlines vl JOIN acct_vouchers v ON v.guid = vl.voucher_guid
			WHERE vl.ledger = ? AND v.cancelled = 0 AND v.date <= ? GROUP BY v.date ORDER BY v.date`, l.name, end.Format(time.DateOnly))
		if err != nil {
			return err
		}
		bal := l.opening
		var worst int64
		var worstDay, first string
		n := 0
		for rows.Next() {
			var d string
			var amt int64
			rows.Scan(&d, &amt)
			bal += amt
			if d[:7] >= p.From && bal > 0 { // Cr balance = negative cash
				n++
				if first == "" {
					first = d
				}
				if bal > worst {
					worst, worstDay = bal, d
				}
			}
		}
		rows.Close()
		if n > 0 {
			r.Issues = append(r.Issues, Issue{Date: ddmmyyyy(first), Party: l.name, Amount: worst,
				Detail: fmt.Sprintf("negative on %d day(s) with entries; lowest -%s on %s", n, rupees(worst), ddmmyyyy(worstDay))})
		}
	}
	r.Note = "Amount is the most negative balance; days counted are days with entries."
	return nil
}

// suspense lists suspense ledgers with a balance at the period end or entries in it.
func suspense(db *sql.DB, ls map[string]*ledger, p report.Period, r *Result) error {
	end, _ := time.Parse("02-01-2006", p.End())
	for _, l := range ls {
		if !l.suspense {
			continue
		}
		var bal int64
		var n int
		db.QueryRow(`SELECT COALESCE(SUM(vl.amount),0), COALESCE(SUM(substr(v.date,1,7) >= ?),0) FROM vlines vl
			JOIN acct_vouchers v ON v.guid = vl.voucher_guid WHERE vl.ledger = ? AND v.cancelled = 0 AND v.date <= ?`,
			p.From, l.name, end.Format(time.DateOnly)).Scan(&bal, &n)
		bal += l.opening
		if bal != 0 || n > 0 {
			side := "Cr"
			if bal < 0 {
				side = "Dr"
			}
			r.Issues = append(r.Issues, Issue{Party: l.name, Amount: abs(bal),
				Detail: fmt.Sprintf("balance %s %s at %s; %d entries in the period", rupees(abs(bal)), side, p.End(), n)})
		}
	}
	return nil
}

// ageingChecks: MSME creditors beyond 45 days and debtors beyond 180 days, as on the end.
func ageingChecks(db *sql.DB, asOf time.Time, msme, old *Result) error {
	pay, err := report.AgeParties(db, "Sundry Creditors", 1, asOf)
	if err != nil {
		return err
	}
	tagged := false
	for _, p := range pay.Parties {
		tagged = tagged || p.MSME
		if p.OverMSME > 0 {
			msme.Issues = append(msme.Issues, Issue{Date: asOf.Format("02-01-2006"), Party: p.Name, Amount: p.OverMSME,
				Detail: fmt.Sprintf("of %s owed, older than %d days", rupees(p.Total), Limits.MSMEDays)})
		}
	}
	if !tagged {
		msme.Note = "No creditor is tagged 'creditor msme' in the ledger mapping; tag MSME suppliers to use this check."
	} else {
		msme.Note = "Aged by FIFO on entries up to the period end (or Tally's open bills when the end is today). Written agreements up to 45 days are not known to the tool."
	}
	recv, err := report.AgeParties(db, "Sundry Debtors", -1, asOf)
	if err != nil {
		return err
	}
	last := len(report.Buckets) - 1
	for _, p := range recv.Parties {
		if v := p.By[last]; v > 0 {
			old.Issues = append(old.Issues, Issue{Date: asOf.Format("02-01-2006"), Party: p.Name, Amount: v,
				Detail: fmt.Sprintf("receivable over %d days (of %s owed)", Limits.OldDebtorDays, rupees(p.Total))})
		}
	}
	if pay.Advances > 0 {
		old.Note = fmt.Sprintf("Also: advances to suppliers %s from %d parties (not aged).", rupees(pay.Advances), pay.NAdv)
	}
	return nil
}
