package mapping

// Threshold for one TDS section, in rupees. Deduct when a single payment exceeds Single
// (if set) or the year's total to the party exceeds Annual (if set); for 194I, Monthly.
// Values: Income-tax Act 1961 as amended by Finance Act 2025 (FY 2025-26). The
// Income-tax Act 2025 renumbers sections from 1 Apr 2026; amounts must be re-checked
// against it before Phase 4 uses them for findings.
type Threshold struct {
	Section                 string
	Single, Annual, Monthly int64
	RatePct                 float64 // normal rate (individual/HUF where it differs is in Note)
	Note                    string
}

var Thresholds = map[string]Threshold{
	"194C contractor":       {"194C", 30000, 100000, 0, 2, "1% if payee is individual/HUF"},
	"194J(a) technical":     {"194J", 0, 50000, 0, 2, ""},
	"194J(b) professional":  {"194J", 0, 50000, 0, 10, "director's fees: no threshold"},
	"194H commission":       {"194H", 0, 20000, 0, 2, ""},
	"194I(a) plant rent":    {"194I", 0, 0, 50000, 2, "per month or part of a month"},
	"194I(b) building rent": {"194I", 0, 0, 50000, 10, "per month or part of a month"},
	"194A interest":         {"194A", 0, 10000, 0, 10, "non-bank payer; 50,000 for banks (1 lakh senior citizens)"},
	"194Q purchase":         {"194Q", 0, 5000000, 0, 0.1, "only above 50 lakh, buyer turnover > 10 crore"},
}

// Due reports whether TDS applies to a payment, given the section tag, the payment and
// the total already paid to the same party in the year (or in the month for 194I).
func Due(tag string, payment, paidBefore int64) bool {
	t, ok := Thresholds[tag]
	if !ok {
		return false
	}
	total := payment + paidBefore
	switch {
	case t.Monthly > 0:
		return total > t.Monthly
	case t.Single > 0 && payment > t.Single:
		return true
	case t.Annual > 0:
		return total > t.Annual
	}
	return false
}
