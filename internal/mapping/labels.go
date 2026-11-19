package mapping

// Plain-language questions and choices for the review page (English; Hindi later).

// Question asked for each family.
var Question = map[string]string{
	TDS:  "TDS: which TDS applies when you pay this expense?",
	GST:  "GST: can you take GST input credit on this?",
	Tax:  "Tax ledgers: what is this tax ledger?",
	Flag: "Ledger type: what kind of ledger is this?",
}

// Help shown under each question.
var Help = map[string]string{
	TDS:  "Used later to find payments where TDS should have been deducted.",
	GST:  "Used later to find GST credit taken where it is not allowed (Sec 17(5)), and reverse charge.",
	Tax:  "Used later to total GST input and output, and TDS payable.",
	Flag: "Used later for audit checks: cash limits, loans, MSME payments, suspense, related parties.",
}

// Label for each tag, shown in the dropdown.
var Label = map[string]string{
	"none":                  "No TDS",
	"192 salary":            "Salary to employees (192)",
	"194A interest":         "Interest paid, not to a bank (194A)",
	"194C contractor":       "Contractor / labour / freight / repairs (194C)",
	"194H commission":       "Commission or brokerage (194H)",
	"194I(a) plant rent":    "Rent of machinery or equipment (194I-a)",
	"194I(b) building rent": "Rent of building, office, godown (194I-b)",
	"194J(a) technical":     "Technical services, software (194J-a)",
	"194J(b) professional":  "Professional fees: CA, lawyer, consultant (194J-b)",

	"eligible":      "Yes, credit allowed",
	"blocked 17(5)": "No, blocked: car, food, gifts, club etc. (17(5))",
	"rcm":           "Reverse charge: we pay the GST (RCM)",
	"exempt":        "No GST charged (exempt / nil rated)",
	"non-gst":       "Outside GST: salary, interest, fuel, electricity",

	"cgst input": "CGST input (on purchases)", "sgst input": "SGST input (on purchases)",
	"igst input": "IGST input (on purchases)", "cess input": "Cess input (on purchases)",
	"cgst output": "CGST output (on sales)", "sgst output": "SGST output (on sales)",
	"igst output": "IGST output (on sales)", "cess output": "Cess output (on sales)",
	"gst rcm payable": "GST payable under reverse charge", "tds payable": "TDS deducted, payable to govt",
	"tcs": "TCS", "other": "Other",

	"cash": "Cash", "bank": "Bank account", "debtor": "Customer (debtor)", "creditor": "Supplier (creditor)",
	"creditor msme": "Supplier registered as MSME", "related party": "Related party: partner, director, relative",
	"capital": "Capital", "loan taken": "Loan taken", "loan given": "Loan given", "suspense": "Suspense",
	"fixed asset": "Fixed asset", "sales": "Sales", "purchase": "Purchase", "income": "Other income",
	"expense": "Expense", "salary": "Salary / staff cost", "tax": "Tax ledger", "stock": "Stock",
}
