// Package mapping suggests one tag per ledger and tag family from the ledger's group chain
// and name. Pure Go rules: no AI, no vouchers, no amounts. The user confirms once.
package mapping

import (
	"regexp"
	"strings"
)

// Families of tags. A ledger gets a row only in the families that apply to it.
const (
	TDS  = "tds"  // TDS section for expense / purchase ledgers
	GST  = "gst"  // GST input treatment for expense / purchase / fixed-asset ledgers
	Tax  = "tax"  // what a Duties & Taxes ledger is
	Flag = "flag" // nature of the ledger for audit checks (every ledger)
)

// Families in display order.
var Families = []string{Flag, TDS, GST, Tax}

// Confidence of a suggestion.
const (
	High   = "high"
	Medium = "medium"
	Low    = "low"
)

// Tags allowed per family. Anything else is refused when the user sets a tag.
var Tags = map[string][]string{
	TDS: {"none", "192 salary", "194A interest", "194C contractor", "194H commission",
		"194I(a) plant rent", "194I(b) building rent", "194J(a) technical", "194J(b) professional"},
	GST: {"eligible", "blocked 17(5)", "rcm", "exempt", "non-gst"},
	Tax: {"cgst input", "sgst input", "igst input", "cess input", "cgst output", "sgst output", "igst output",
		"cess output", "gst rcm payable", "tds payable", "tcs", "other"},
	Flag: {"cash", "bank", "debtor", "creditor", "creditor msme", "related party", "capital", "loan taken",
		"loan given", "suspense", "fixed asset", "sales", "purchase", "income", "expense", "salary",
		"tax", "stock", "other"},
}

// Valid reports whether tag is allowed in family (case-insensitive); returns the canonical tag.
func Valid(family, tag string) (string, bool) {
	for _, t := range Tags[family] {
		if strings.EqualFold(strings.TrimSpace(tag), t) {
			return t, true
		}
	}
	return "", false
}

// Suggestion for one ledger in one family.
type Suggestion struct {
	Family, Tag, Conf, Reason string
}

// Ledger as the rules see it: name and its group chain (parent first, then ancestors).
type Ledger struct {
	Name   string
	Groups []string
}

func (l Ledger) under(gs ...string) bool {
	for _, a := range l.Groups {
		for _, g := range gs {
			if strings.EqualFold(a, g) {
				return true
			}
		}
	}
	return false
}

type rule struct {
	re           *regexp.Regexp
	tag, conf, w string
}

// kw builds a word-start match for any of the keywords (so "rent" does not hit "current").
func kw(words ...string) *regexp.Regexp {
	for i, w := range words {
		words[i] = regexp.QuoteMeta(w)
	}
	return regexp.MustCompile(`(?i)(^|[^a-z])(` + strings.Join(words, "|") + `)`)
}

// First match wins, so more specific rules come first.
var tdsRules = []rule{
	{kw("interest on tds", "interest on gst", "interest on late", "interest on income tax", "interest on tax", "intrest on tds", "intrest on gst", "late payment interest"), "none", High, "interest on late tax payment: no TDS"},
	{kw("interest on capital", "intrest on capital", "partner", "bank interest", "bank intrest", "interest to bank", "od interest", "cc interest"), "none", High, "interest to partners or banks: no TDS"},
	{kw("employees share", "employers share", "employee share", "employer share", "pf", "esi", "esic", "provident", "late fee", "gratuity", "medical", "food", "refreshment", "da)", "allowance", "scrap", "security deposit", "deposite", "mouse", "wheels"), "none", Medium, "no TDS normally (statutory, staff or goods)"},
	{kw("salary", "salaries", "bonus", "stipend", "director remuneration", "remuneration"), "192 salary", High, "salary"},
	{kw("wages", "majdoori", "mazdoori"), "194C contractor", Low, "wages: 192 if employees, 194C if contract labour"},
	{kw("machine hire", "machinery hire", "plant hire", "equipment hire", "equipment rent", "machinery rent", "vehicle hire", "crane"), "194I(a) plant rent", High, "hire of plant/machinery"},
	{kw("rent", "kiraya", "lease", "godown"), "194I(b) building rent", High, "rent"},
	{kw("commission", "brokerage", "dalali"), "194H commission", High, "commission/brokerage"},
	{kw("technical", "software", "it service", "call centre", "royalty"), "194J(a) technical", Medium, "technical services"},
	{kw("professional", "legal", "consult", "audit fee", "advocate", "lawyer", "architect", "retainer", "ca fee", "accounting charges", "valuation"), "194J(b) professional", High, "professional fees"},
	{kw("contract", "labour", "labor", "job work", "jobwork", "fabrication", "cleaning", "handling", "clearance", "advertis", "printing", "catering", "housekeeping", "security", "manpower", "repair", "maintenance", "installation"), "194C contractor", Medium, "work contract"},
	{kw("freight", "transport", "cartage", "carriage", "courier", "couriour", "shipment", "loading", "unloading", "hamali"), "194C contractor", Medium, "freight (194C unless transporter gave PAN declaration)"},
	{kw("interest", "intrest"), "194A interest", Medium, "interest paid"},
	{kw("electricity", "power", "telephone", "mobile", "internet", "broadband", "conveyance", "convence", "travel", "stationary", "server", "petrol", "diesel", "fuel", "bank charge", "depreciation", "round off", "rounding", "discount", "postage", "stationery", "office exp", "tea", "staff welfare", "donation", "insurance", "gst", "tax", "penalty", "fine", "stamp", "bad debt", "loss", "exchange"), "none", High, "no TDS normally"},
}

var gstRules = []rule{
	{kw("rcm", "reverse charge", "gta"), "rcm", High, "reverse charge"},
	{kw("legal", "advocate", "lawyer"), "rcm", Medium, "advocate fees are under RCM"},
	{kw("salary", "salaries", "wages", "bonus", "stipend", "remuneration", "interest", "intrest", "employee exp", "allowance", "da)", "esic", "late fee", "security deposit", "deposite", "scrap", "depreciation", "electricity", "petrol", "diesel", "fuel", "round off", "rounding", "donation", "penalty", "fine", "stamp", "rates and taxes", "income tax", "tds", "esi", "provident", "pf", "bad debt", "discount", "loss", "exchange", "municipal"), "non-gst", High, "outside GST"},
	{kw("motor car", "car ", "vehicle", "emp. benifit", "employee benefit", "food", "canteen", "refreshment", "tea", "meal", "staff welfare", "club", "membership", "gift", "diwali", "festival", "beauty", "health insurance", "life insurance", "medical", "cab", "guest house", "holiday"), "blocked 17(5)", Medium, "likely blocked credit u/s 17(5)"},
	{kw("insurance", "rent", "repair", "bank charge", "cartage", "freight", "transport", "courier", "couriour", "shipment", "labour", "fabrication", "service", "server", "handling", "clearance", "cleaning", "stationary", "office exp", "conveyance", "convence", "machine", "laptop", "computer", "printer", "furniture", "chair", "table", "cooler", "ups", "camera", "gps", "battery", "bettrey", "stabilizer", "lock", "generator", "genrator", "telephone", "internet", "professional", "consult", "freight", "commission", "advertis", "printing", "purchase", "software", "stationery"), "eligible", Medium, "normal input service/goods"},
}

func match(rs []rule, name string) (rule, bool) {
	for _, r := range rs {
		if r.re.MatchString(name) {
			return r, true
		}
	}
	return rule{}, false
}

var (
	reCGST, reSGST = kw("cgst", "central tax"), kw("sgst", "utgst", "state tax")
	reIGST, reCess = kw("igst", "integrated"), kw("cess")
	reDuty         = kw("surcharge", "duty", "custom", "challan", "gst return", "excise", "vat")
	reIn, reOut    = kw("input", "itc", "in ", "p.", "purchase", "recoverable", "receivable", "credit"), kw("output", "out", "payable", "liability")
	reTDS, reTCS   = kw("tds", "194", "192"), kw("tcs", "206c")
	reRCM          = kw("rcm", "reverse charge")
)

func taxTag(name string) Suggestion {
	n := name + " "
	if reTCS.MatchString(n) {
		return Suggestion{Tax, "tcs", High, "TCS"}
	}
	if reTDS.MatchString(n) {
		return Suggestion{Tax, "tds payable", High, "TDS"}
	}
	if reRCM.MatchString(n) {
		return Suggestion{Tax, "gst rcm payable", High, "GST under RCM"}
	}
	kind := ""
	switch {
	case reCGST.MatchString(n):
		kind = "cgst"
	case reSGST.MatchString(n):
		kind = "sgst"
	case reIGST.MatchString(n):
		kind = "igst"
	case reCess.MatchString(n):
		kind = "cess"
	case reDuty.MatchString(n):
		return Suggestion{Tax, "other", Medium, "other duty or GST payment/return ledger"}
	default:
		return Suggestion{Tax, "other", Low, "not a recognised GST/TDS ledger name"}
	}
	switch {
	case reIn.MatchString(n):
		return Suggestion{Tax, kind + " input", High, strings.ToUpper(kind) + " input"}
	case reOut.MatchString(n):
		return Suggestion{Tax, kind + " output", High, strings.ToUpper(kind) + " output"}
	}
	// Many books keep one ledger per tax for both sides; Tally nets them. Treat as output.
	return Suggestion{Tax, kind + " output", Low, "no input/output in the name"}
}

func flagTag(l Ledger) Suggestion {
	s := func(tag, conf, why string) Suggestion { return Suggestion{Flag, tag, conf, why} }
	switch {
	case l.under("Cash-in-Hand"):
		return s("cash", High, "group Cash-in-Hand")
	case l.under("Bank Accounts", "Bank OD A/c", "Bank OCC A/c"):
		return s("bank", High, "bank group")
	case l.under("Suspense A/c"):
		return s("suspense", High, "group Suspense A/c")
	case l.under("Sundry Debtors"):
		return s("debtor", High, "group Sundry Debtors")
	case l.under("Sundry Creditors"):
		return s("creditor", Medium, "group Sundry Creditors; change to 'creditor msme' for MSME suppliers")
	case l.under("Capital Account"):
		return s("related party", Medium, "partner/proprietor capital")
	case l.under("Loans (Liability)", "Unsecured Loans", "Secured Loans"):
		return s("loan taken", High, "loan group (269SS/269T)")
	case l.under("Loans & Advances (Asset)"):
		return s("loan given", Medium, "loans & advances (269SS/269T if a loan)")
	case l.under("Fixed Assets"):
		return s("fixed asset", High, "group Fixed Assets")
	case l.under("Duties & Taxes"):
		return s("tax", High, "group Duties & Taxes")
	case l.under("Stock-in-Hand"):
		return s("stock", High, "group Stock-in-Hand")
	case l.under("Sales Accounts"):
		return s("sales", High, "group Sales Accounts")
	case l.under("Purchase Accounts"):
		return s("purchase", High, "group Purchase Accounts")
	case l.under("Direct Incomes", "Indirect Incomes"):
		return s("income", High, "income group")
	case l.under("Direct Expenses", "Indirect Expenses"):
		if r, ok := match(tdsRules[:1], l.Name); ok {
			return s("salary", High, r.w)
		}
		return s("expense", High, "expense group")
	}
	return s("other", Medium, "no special nature")
}

// Suggest returns one suggestion per family that applies to the ledger.
// primary holds Tally's reserved groups; their names say nothing beyond what the group
// checks already use, so only the user's own sub-groups are matched as text.
var primary = map[string]bool{}

func init() {
	for _, g := range []string{"Bank Accounts", "Bank OCC A/c", "Bank OD A/c", "Branch / Divisions", "Capital Account",
		"Cash-in-Hand", "Current Assets", "Current Liabilities", "Deposits (Asset)", "Direct Expenses", "Direct Incomes",
		"Duties & Taxes", "Expenses (Direct)", "Expenses (Indirect)", "Fixed Assets", "Income (Direct)", "Income (Indirect)",
		"Indirect Expenses", "Indirect Incomes", "Investments", "Loans & Advances (Asset)", "Loans (Liability)",
		"Misc. Expenses (ASSET)", "Provisions", "Purchase Accounts", "Reserves & Surplus", "Retained Earnings",
		"Sales Accounts", "Secured Loans", "Stock-in-Hand", "Sundry Creditors", "Sundry Debtors", "Suspense A/c", "Unsecured Loans"} {
		primary[strings.ToLower(g)] = true
	}
}

// matchLedger tries the ledger name, then its own sub-group names (one level less sure).
func matchLedger(rs []rule, l Ledger) (Suggestion, bool) {
	if r, ok := match(rs, l.Name+" "); ok {
		return Suggestion{Tag: r.tag, Conf: r.conf, Reason: r.w}, true
	}
	for _, g := range l.Groups {
		if primary[strings.ToLower(g)] {
			continue
		}
		if r, ok := match(rs, g+" "); ok {
			c := r.conf
			if c == High {
				c = Medium
			} else {
				c = Low
			}
			return Suggestion{Tag: r.tag, Conf: c, Reason: r.w + " (from group " + g + ")"}, true
		}
	}
	return Suggestion{}, false
}

var reRate = regexp.MustCompile(`@\s*\d+(\.\d+)?\s*%`)

func Suggest(l Ledger) []Suggestion {
	out := []Suggestion{flagTag(l)}
	expense := l.under("Direct Expenses", "Indirect Expenses")
	purchase := l.under("Purchase Accounts")
	add := func(fam string, s Suggestion) { s.Family = fam; out = append(out, s) }
	if expense {
		if s, ok := matchLedger(tdsRules, l); ok {
			add(TDS, s)
		} else {
			add(TDS, Suggestion{Tag: "none", Conf: Low, Reason: "no rule matched"})
		}
	}
	// No TDS question on purchase/goods ledgers: 194Q depends on the supplier (yearly
	// purchases from one seller over 50 lakh) and the buyer's turnover, not on the ledger.
	// Phase 4 checks it per supplier.
	if expense || purchase || l.under("Fixed Assets") {
		s, ok := matchLedger(gstRules, l)
		switch {
		case ok:
		case reRate.MatchString(l.Name):
			s = Suggestion{Tag: "eligible", Conf: Medium, Reason: "GST rate in ledger name"}
		case purchase:
			s = Suggestion{Tag: "eligible", Conf: Medium, Reason: "purchase of goods"}
		case l.under("Fixed Assets"):
			s = Suggestion{Tag: "eligible", Conf: Medium, Reason: "capital goods"}
		default:
			s = Suggestion{Tag: "eligible", Conf: Low, Reason: "no rule matched"}
		}
		add(GST, s)
	}
	switch {
	case l.under("Duties & Taxes"):
		out = append(out, taxTag(l.Name))
	case l.under("Current Liabilities") && (reTDS.MatchString(l.Name+" ") || reTCS.MatchString(l.Name+" ")):
		// TDS/TCS payable kept under Provisions or Current Liabilities (seen on a pilot).
		out = append(out, taxTag(l.Name))
	}
	return out
}
