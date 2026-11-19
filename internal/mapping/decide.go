package mapping

import (
	"fmt"
	"strings"
)

// Evidence is what the books show for one ledger (amounts in paise, debits positive).
type Evidence struct {
	Entries             int
	Total, MaxYear      int64   // cost ledgers: all debits; largest financial year
	GSTShare, TDSShare  float64 // share of debits with GST input / TDS deducted
	WithSales, WithCost int     // tax ledgers: entries next to sales vs cost lines
}

// Material is the yearly amount above which a doubtful answer is worth the user's time.
const Material = 50000 * 100

// Decision is the suggestion after looking at the books. Ask=false means the tool
// decided and the user need not check (they still can).
type Decision struct {
	Tag, Reason string
	Ask         bool
}

func rs(p int64) string {
	r := p / 100
	switch {
	case r >= 10000000:
		return fmt.Sprintf("%.1f cr", float64(r)/1e7)
	case r >= 100000:
		return fmt.Sprintf("%.1f lakh", float64(r)/1e5)
	}
	return fmt.Sprintf("%d", r)
}

func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

// Decide combines the name/group suggestion with the books' evidence.
func Decide(s Suggestion, e *Evidence) Decision {
	if e == nil || e.Entries == 0 {
		return Decision{s.Tag, "not used in any entry", false}
	}
	switch s.Family {
	case Flag:
		return Decision{s.Tag, s.Reason, false} // from Tally's groups
	case Tax:
		if s.Tag == "tds payable" || s.Tag == "tcs" || s.Tag == "gst rcm payable" || s.Tag == "other" {
			return Decision{s.Tag, s.Reason, false}
		}
		kind := strings.Fields(s.Tag)[0]
		if s.Conf == High {
			// The name says input or output: keep it. Usage over several years can mislead
			// (a pilot's "CGST Output" was used once in a purchase). Reports split mixed
			// ledgers per voucher anyway.
			return Decision{s.Tag, fmt.Sprintf("%s; used in %d sales and %d purchase/expense entries", s.Reason, e.WithSales, e.WithCost), false}
		}
		switch {
		case e.WithSales > e.WithCost:
			return Decision{kind + " output", fmt.Sprintf("used in %d sales entries", e.WithSales), false}
		case e.WithCost > e.WithSales:
			return Decision{kind + " input", fmt.Sprintf("used in %d purchase/expense entries", e.WithCost), false}
		}
		return Decision{s.Tag, s.Reason + "; only in journals", s.Conf == Low}
	case TDS:
		big := e.MaxYear > Material
		took := e.TDSShare >= 0.3
		switch {
		case took && s.Tag != "none":
			return Decision{s.Tag, "TDS deducted on " + pct(e.TDSShare) + " of it; " + s.Reason, false}
		case took:
			// TDS is deducted per party; the section comes from the party's entries and the
			// 26Q return in the audit (Phase 4), not from the expense ledger. Never ask.
			return Decision{s.Tag, "TDS deducted on " + pct(e.TDSShare) + "; section per party from 26Q in audit", false}
		case s.Tag == "none" || !big:
			return Decision{s.Tag, s.Reason + "; " + rs(e.MaxYear) + " a year", false}
		}
		// A section applies by its nature and TDS is missing: an audit finding (Phase 4),
		// not a mapping question.
		return Decision{s.Tag, s.Reason + "; TDS seen on " + pct(e.TDSShare) + " (checked in audit)", false}
	case GST:
		big := e.MaxYear > Material
		with := e.GSTShare >= 0.5
		switch {
		case with && s.Tag == "blocked 17(5)" && big:
			return Decision{s.Tag, "credit taken on " + pct(e.GSTShare) + " (" + rs(e.MaxYear) + " a year) but looks blocked: " + s.Reason, true}
		case with && s.Tag == "non-gst" && big:
			return Decision{"eligible", "GST input booked on " + pct(e.GSTShare) + ", though the name suggests outside GST", true}
		case with && (s.Tag == "eligible" || s.Tag == "exempt" || !big):
			return Decision{"eligible", "GST input booked on " + pct(e.GSTShare) + " of it", false}
		case !with && s.Tag == "eligible":
			return Decision{"exempt", "no GST booked on " + pct(1-e.GSTShare) + " of it", false}
		}
		return Decision{s.Tag, s.Reason, false}
	}
	return Decision{s.Tag, s.Reason, s.Conf == Low}
}
