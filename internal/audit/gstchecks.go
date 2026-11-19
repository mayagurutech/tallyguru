package audit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/gst"
)

// invalidGSTIN lists party ledgers whose GSTIN fails the format, state code or check digit,
// or whose GSTIN state differs from the state on the ledger.
func invalidGSTIN(ls map[string]*ledger, r *Result) {
	n := 0
	for _, l := range ls {
		if l.gstin == "" {
			continue
		}
		n++
		if p := gst.Problem(l.gstin); p != "" {
			r.Issues = append(r.Issues, Issue{Party: l.name, Detail: "GSTIN " + p})
			continue
		}
		if l.state != "" && !gst.SameState(gst.StateOf(l.gstin), l.state) {
			r.Issues = append(r.Issues, Issue{Party: l.name,
				Detail: fmt.Sprintf("GSTIN is for %s but the ledger's state is %s", gst.StateOf(l.gstin), l.state)})
		}
	}
	sort.Slice(r.Issues, func(i, j int) bool { return r.Issues[i].Party < r.Issues[j].Party })
	r.Note = fmt.Sprintf("%d ledgers with a GSTIN checked. Validity on the GST portal (active/cancelled) is not checked here.", n)
}

// gstLine classifies a voucher's GST lines: IGST, CGST/SGST (local), or none.
func (s *state) gstKinds(v voucher) (igst, local bool) {
	for _, l := range v.lines {
		t := s.led(l.ledger).taxTag
		switch {
		case strings.HasPrefix(t, "igst"):
			igst = true
		case strings.HasPrefix(t, "cgst"), strings.HasPrefix(t, "sgst"):
			local = true
		}
	}
	return
}

// checkGSTState compares the tax charged with the party's state (from a valid GSTIN, else
// the ledger). Foreign parties are left out (exports/imports are IGST by nature).
func (s *state) checkGSTState(v voucher, _ *ledger) {
	igst, local := s.gstKinds(v)
	if !igst && !local {
		return
	}
	// The party must be a customer or supplier ledger (largest in the entry). Expense and
	// asset ledgers carry the company's state by default in Tally (pilot: "Apple MacBook"
	// paid from bank showed as a same-state party), so entries without one are not judged.
	var cp *ledger
	var big int64
	for _, l := range v.lines {
		if x := s.led(l.ledger); (x.debtor || x.creditor) && abs(l.amount) > big {
			cp, big = x, abs(l.amount)
		}
	}
	if cp == nil {
		s.gstNoParty++
		return
	}
	ps := cp.partyState()
	if cp.foreign || s.companyState == "" || ps == "" {
		s.gstNoState++
		return
	}
	same := gst.SameState(ps, s.companyState)
	why := ""
	switch {
	case same && igst && !local:
		why = fmt.Sprintf("IGST charged; party in %s, same state as the company", ps)
	case !same && local && !igst:
		why = fmt.Sprintf("CGST+SGST charged; party in %s, company in %s", ps, s.companyState)
	default:
		return
	}
	s.gstState = append(s.gstState, Issue{Date: ddmmyyyy(v.date), Party: cp.name, Amount: v.value,
		Detail: v.typ + " " + v.number + ": " + why, Vouchers: []string{v.guid}})
}

func (s *state) finishGST(rs map[string]*Result) {
	r := rs["igst_vs_state"]
	r.Issues = s.gstState
	r.Note = "Party state from a valid GSTIN, else the ledger. Place of supply can differ (services, bill-to/ship-to); judge each."
	if s.companyState == "" {
		r.NotAvailable = true
		r.Note = "The company's state is not known from Tally yet (sync again to read it)."
	} else if s.gstNoState+s.gstNoParty > 0 {
		r.Note += fmt.Sprintf(" Not checked: %d GST entries with no customer/supplier ledger (paid directly), %d with the party's state unknown or a foreign party.",
			s.gstNoParty, s.gstNoState)
	}
	f := rs["foreign_parties"]
	for _, x := range s.foreignAgg {
		f.Issues = append(f.Issues, *x)
	}
	f.Note = "Parties whose country is not India. They are left out of the 194C and GST state checks; TDS on payments to non-residents and GST on imports need the auditor's review."
}
