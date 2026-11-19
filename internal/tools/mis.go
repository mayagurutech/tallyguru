package tools

import (
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/report"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// misLabels in English and Hindi (Hindi is a draft, like the rest of i18n).
var misLabels = map[string][2]string{
	"title":    {"owner's summary", "मालिक के लिए सारांश"},
	"ytd":      {"Year to date", "वर्ष की शुरुआत से अब तक"},
	"sales":    {"Sales", "बिक्री"},
	"expenses": {"Expenses", "खर्च"},
	"profit":   {"Profit", "मुनाफ़ा"},
	"loss":     {"Loss", "घाटा"},
	"month":    {"Latest month", "पिछला महीना"},
	"vs":       {"vs", "बनाम"},
	"cash":     {"Cash and bank", "नकद और बैंक"},
	"start":    {"at year start", "वर्ष की शुरुआत में"},
	"recv":     {"To receive from customers", "ग्राहकों से लेना है"},
	"old":      {"over 180 days", "180 दिन से पुराना"},
	"pay":      {"To pay suppliers", "सप्लायरों को देना है"},
	"adv":      {"advances given to suppliers", "सप्लायरों को दिया एडवांस"},
	"loans":    {"Loans outstanding", "बकाया लोन"},
	"gst":      {"GST payable (books, year to date)", "देय GST (बहीखाते से, वर्ष में अब तक)"},
	"tds":      {"TDS payable now", "अभी देय TDS"},
	"topd":     {"Largest customers to collect from", "सबसे बड़े बकाया ग्राहक"},
	"topc":     {"Largest suppliers to pay", "सबसे बड़े बकाया सप्लायर"},
	"alerts":   {"Points to look at", "ध्यान देने योग्य बातें"},
	"none":     {"none", "कोई नहीं"},
	"note":     {"Figures from the books in Tally; check before relying on them.", "आँकड़े Tally के बहीखातों से हैं; भरोसा करने से पहले जाँच लें।"},
}

func (e *Env) lbl(k string) string {
	l := misLabels[k]
	if e.Cfg.Language == "hi" {
		return l[1]
	}
	return l[0]
}

func pct(now, before int64) string {
	if before == 0 {
		return ""
	}
	return fmt.Sprintf(" (%+.0f%%)", float64(now-before)*100/float64(abs64(before)))
}

func (e *Env) mis(db *store.DB) (string, error) {
	m, err := report.BuildMIS(db.DB, db.Label.Company, e.Now())
	if err != nil {
		return "", err
	}
	var b strings.Builder
	pw := func(v int64) string {
		if v < 0 {
			return e.lbl("loss") + " " + amt(-v)
		}
		return e.lbl("profit") + " " + amt(v)
	}
	fmt.Fprintf(&b, "%s, %s (%s)\n", m.Company, e.lbl("title"), m.P.Label())
	fmt.Fprintf(&b, "%s: %s %s | %s %s | %s", e.lbl("ytd"), e.lbl("sales"), amt(m.Sales), e.lbl("expenses"), amt(m.Expenses), pw(m.Profit))
	if m.Sales > 0 {
		fmt.Fprintf(&b, " (%.1f%%)", float64(m.Profit)*100/float64(m.Sales))
	}
	fmt.Fprintf(&b, "\n%s (%s %s %s): %s %s%s | %s\n", e.lbl("month"), monthName(m.LastMonth), e.lbl("vs"), monthName(m.PrevMonth),
		e.lbl("sales"), amt(m.SalesLast), pct(m.SalesLast, m.SalesPrev), pw(m.ProfitLast))
	fmt.Fprintf(&b, "%s: %s (%s %s)\n", e.lbl("cash"), bracket(m.Cash), bracket(m.CashStart), e.lbl("start"))
	fmt.Fprintf(&b, "%s: %s (%s %s)\n", e.lbl("recv"), amt(m.Receivable), amt(m.RecvOld), e.lbl("old"))
	fmt.Fprintf(&b, "%s: %s", e.lbl("pay"), amt(m.Payable))
	if m.SupplierAdvances > 0 {
		fmt.Fprintf(&b, " (%s %s)", amt(m.SupplierAdvances), e.lbl("adv"))
	}
	fmt.Fprintf(&b, "\n%s: %s | %s: %s | %s: %s\n", e.lbl("loans"), amt(m.Borrowings), e.lbl("gst"), bracket(m.GSTNet), e.lbl("tds"), bracket(m.TDSPayable))
	for i, ls := range [][]report.Line{m.TopDebtors, m.TopCreditors} {
		if len(ls) == 0 {
			continue
		}
		var parts []string
		group := []string{"Sundry Debtors", "Sundry Creditors"}[i]
		for _, l := range ls {
			parts = append(parts, e.show(db, l.Label, group)+" "+amt(l.Amount))
		}
		fmt.Fprintf(&b, "%s: %s\n", e.lbl([]string{"topd", "topc"}[i]), strings.Join(parts, "; "))
	}
	fmt.Fprintf(&b, "%s:", e.lbl("alerts"))
	if len(m.Alerts) == 0 {
		b.WriteString(" " + e.lbl("none"))
	}
	for _, a := range m.Alerts {
		b.WriteString("\n- " + a)
	}
	b.WriteString("\n" + e.lbl("note"))
	return b.String(), nil
}
