package report

import "regexp"

// Schedule III (Companies Act, 2013) heads. Ledgers are placed by their Tally group and,
// where a group mixes heads, by balance side (a debtor with a credit balance is an advance
// from a customer). Name-based heads are marked for review: the layout assists, the
// accountant decides.

// Balance-sheet heads in Schedule III order.
var (
	eqHeads = []struct {
		Title string
		Heads []string
	}{
		{"Shareholders' funds", []string{"Share capital / Owners' capital", "Reserves and surplus"}},
		{"Non-current liabilities", []string{"Long-term borrowings", "Long-term provisions"}},
		{"Current liabilities", []string{"Short-term borrowings", "Trade payables - MSME", "Trade payables - others", "Other current liabilities", "Short-term provisions"}},
	}
	assetHeads = []struct {
		Title string
		Heads []string
	}{
		{"Non-current assets", []string{"Property, plant and equipment", "Non-current investments", "Long-term loans and advances"}},
		{"Current assets", []string{"Inventories", "Trade receivables", "Cash and cash equivalents", "Short-term loans and advances", "Other current assets"}},
	}
)

// s3Head places one balance-sheet ledger. cr = credit balance. The second value says
// the head is a liability (true) or an asset.
func s3Head(l bsLedger) (string, bool) {
	cr := l.Bal > 0
	u := l.Under
	switch {
	case u("Sundry Debtors"):
		if cr {
			return "Other current liabilities", true // advances from customers
		}
		return "Trade receivables", false
	case u("Sundry Creditors"):
		if !cr {
			return "Short-term loans and advances", false // advances to suppliers
		}
		if l.MSME {
			return "Trade payables - MSME", true
		}
		return "Trade payables - others", true
	case u("Bank OD A/c"):
		if !cr {
			return "Cash and cash equivalents", false
		}
		return "Short-term borrowings", true
	case u("Bank Accounts"), u("Cash-in-Hand"):
		if cr {
			return "Short-term borrowings", true // overdrawn bank (or negative cash: see audit)
		}
		return "Cash and cash equivalents", false
	case u("Duties & Taxes"):
		if cr {
			return "Other current liabilities", true
		}
		return "Other current assets", false // balances with government
	case u("Provisions"):
		return "Short-term provisions", true
	case u("Secured Loans"), u("Unsecured Loans"), u("Loans (Liability)"):
		if !cr {
			return "Short-term loans and advances", false
		}
		return "Long-term borrowings", true
	case u("Reserves & Surplus"):
		return "Reserves and surplus", true
	case u("Capital Account"):
		return "Share capital / Owners' capital", true
	case u("Stock-in-Hand"):
		return "Inventories", false
	case u("Deposits (Asset)"):
		return "Long-term loans and advances", false
	case u("Loans & Advances (Asset)"):
		return "Short-term loans and advances", false
	case u("Fixed Assets"):
		return "Property, plant and equipment", false
	case u("Investments"):
		return "Non-current investments", false
	}
	if cr {
		return "Other current liabilities", true
	}
	return "Other current assets", false
}

// S3Heads totals every balance-sheet head at the period end, natural side positive
// (liabilities Cr, assets Dr). Reserves include the Profit & Loss A/c.
func (b *Books) S3Heads() (amt map[string]int64, plOpen, profit, suspense int64) {
	ls, plOpen := b.bsLedgers()
	amt = map[string]int64{}
	for _, l := range ls {
		h, liab := s3Head(l)
		if liab {
			amt[h] += l.Bal
		} else {
			amt[h] += -l.Bal
		}
		if l.Under("Suspense A/c") {
			suspense += l.Bal
		}
	}
	profit = b.ProfitToDate()
	amt["Reserves and surplus"] += plOpen + profit
	return amt, plOpen, profit, suspense
}

// S3BS builds the Schedule III balance sheet. Notes list what needs the accountant's review.
func (b *Books) S3BS() (bs BS, notes []string) {
	amt, plOpen, profit, suspense := b.S3Heads()
	for _, g := range eqHeads {
		s := Section{Title: g.Title}
		for _, h := range g.Heads {
			s.add(h, amt[h], "")
		}
		bs.Liab = append(bs.Liab, s)
	}
	for _, g := range assetHeads {
		s := Section{Title: g.Title}
		for _, h := range g.Heads {
			s.add(h, amt[h], "")
		}
		bs.Assets = append(bs.Assets, s)
	}
	var l, a int64
	for _, s := range bs.Liab {
		l += s.Total
	}
	for _, s := range bs.Assets {
		a += s.Total
	}
	bs.Diff, bs.PLOpen, bs.PL = l-a, plOpen, profit
	notes = append(notes, "Borrowings are shown as long-term; move current maturities to short-term borrowings.")
	if amt["Trade payables - MSME"] == 0 {
		notes = append(notes, "No creditor is tagged MSME in the ledger mapping, so all trade payables show as 'others'.")
	}
	if suspense != 0 {
		notes = append(notes, "Suspense A/c has a balance ("+money(abs(suspense))+"); clear it before finalising.")
	}
	return bs, notes
}

// Name patterns for Schedule III expense heads (reviewed by the accountant).
var (
	reEmployee = regexp.MustCompile(`(?i)salar|wage|bonus|staff|employee|provident|\bpf\b|\besi|gratuity|leave encash|remuneration`)
	reFinance  = regexp.MustCompile(`(?i)interest|bank charge|processing fee|loan charge|finance charge`)
	reDep      = regexp.MustCompile(`(?i)depreciation|amorti[sz]`)
	reTax      = regexp.MustCompile(`(?i)income.?tax|current tax|deferred tax|tax expense|provision for tax`)
)

// S3PL builds the Schedule III statement of profit and loss as one section of lines.
func (b *Books) S3PL() (inc, exp Section, pbt, tax int64, notes []string) {
	inc.Title, exp.Title = "Income", "Expenses"
	var rev, other, purch, emp, fin, dep, oth int64
	for _, l := range b.Ledgers {
		if !l.Revenue {
			continue
		}
		v := b.periodAmt(l) // credit positive
		if v == 0 {
			continue
		}
		switch {
		case l.Under("Sales Accounts"), l.Under("Direct Incomes"):
			rev += v
		case l.Under("Indirect Incomes"):
			other += v
		case l.Under("Purchase Accounts"):
			purch -= v
		case reTax.MatchString(l.Name):
			tax -= v
		case l.Salary || reEmployee.MatchString(l.Name):
			emp -= v
		case reDep.MatchString(l.Name):
			dep -= v
		case reFinance.MatchString(l.Name) && v < 0:
			fin -= v
		default:
			oth -= v
		}
	}
	open, close := b.stockAdj()
	inc.add("Revenue from operations", rev, "")
	inc.add("Other income", other, "")
	exp.add("Purchases / cost of materials", purch, "")
	exp.add("Changes in inventories", open-close, "")
	exp.add("Employee benefits expense", emp, "")
	exp.add("Finance costs", fin, "")
	exp.add("Depreciation and amortisation", dep, "")
	exp.add("Other expenses", oth, "")
	pbt = inc.Total - exp.Total
	notes = append(notes, "Employee, finance, depreciation and tax heads are picked from ledger names and the salary tag; review them.")
	if dep == 0 {
		notes = append(notes, "No depreciation found in the books for this period.")
	}
	return inc, exp, pbt, tax, notes
}
