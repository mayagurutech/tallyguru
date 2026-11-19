package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
)

func (e *Env) monthSummary(a map[string]any) (string, error) {
	db, err := e.open(a, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	months := 12
	if f, ok := a["months"].(float64); ok && f >= 1 {
		months = min(int(f), 36)
	}
	rows, err := db.Query(`SELECT month, metric, amount FROM month_summary
		WHERE month IN (SELECT DISTINCT month FROM month_summary ORDER BY month DESC LIMIT ?)
		ORDER BY month`, months)
	if err != nil {
		return "", err
	}
	data := map[string]map[string]int64{}
	var order []string
	for rows.Next() {
		var m, k string
		var v int64
		rows.Scan(&m, &k, &v)
		if data[m] == nil {
			data[m] = map[string]int64{}
			order = append(order, m)
		}
		data[m][k] = v
	}
	rows.Close()
	if len(order) == 0 {
		return "No transactions in the synced period.", nil
	}
	// Columns: only metrics that have any value, to keep the table small.
	var cols []struct{ ID, Label string }
	for _, mt := range store.Metrics {
		for _, m := range order {
			if data[m][mt.ID] != 0 {
				cols = append(cols, mt)
				break
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, month-wise (amounts in Rs; expenses and GST are net)\nMonth", db.Label.Company)
	for _, c := range cols {
		b.WriteString(" | " + short(c.ID))
	}
	b.WriteString("\n")
	tot := map[string]int64{}
	for _, m := range order {
		t, _ := time.Parse("2006-01", m)
		b.WriteString(t.Format("Jan 2006"))
		for _, c := range cols {
			v := data[m][c.ID]
			tot[c.ID] += v
			b.WriteString(" | " + signed(v))
		}
		b.WriteString("\n")
	}
	b.WriteString("Total")
	for _, c := range cols {
		b.WriteString(" | " + signed(tot[c.ID]))
	}
	b.WriteString("\nGST columns: ledgers under Duties & Taxes named *GST*, until ledger mapping (Phase 2).")
	return b.String(), nil
}

func short(id string) string {
	return map[string]string{"sales": "Sales", "purchases": "Purchases", "direct_exp": "Direct exp",
		"indirect_exp": "Indirect exp", "other_income": "Other inc", "gst_cr": "GST Cr", "gst_dr": "GST Dr",
		"receipts": "Receipts", "payments": "Payments"}[id]
}

func signed(p int64) string {
	if p < 0 {
		return "-" + amt(-p)
	}
	return amt(p)
}
