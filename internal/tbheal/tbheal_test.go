package tbheal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/sync"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tbcheck"
)

type fv struct {
	guid, date string
	paise      int64
}

// fakeTally serves vouchers (Cash Dr / Sales Cr) and ledger balances computed from them, except
// that vouchers in skip are left out of Tally's balances (a cause no rule knows).
type fakeTally struct {
	vs   []fv
	skip map[string]bool
}

func amt(p int64) string { return fmt.Sprintf("%d.%02d", p/100, p%100) }

func (f *fakeTally) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	cut := func(a string) string {
		i := strings.Index(body, a)
		if i < 0 {
			return ""
		}
		s := body[i+len(a):]
		return s[:strings.Index(s, "<")]
	}
	td := func(s string) string { t, _ := time.Parse("2-Jan-2006", s); return t.Format("20060102") }
	from, to := td(cut(`<SVFROMDATE TYPE="Date">`)), td(cut(`<SVTODATE TYPE="Date">`))
	var out strings.Builder
	out.WriteString("<ENVELOPE><BODY><DATA><COLLECTION>")
	switch {
	case strings.Contains(body, "GuruLedgerBal"):
		var s int64
		for _, v := range f.vs {
			if v.date >= from && v.date <= to && !f.skip[v.guid] {
				s += v.paise
			}
		}
		fmt.Fprintf(&out, `<LEDGER NAME="Cash"><GUID>L1</GUID><CLOSINGBALANCE>-%s</CLOSINGBALANCE></LEDGER>`, amt(s))
		fmt.Fprintf(&out, `<LEDGER NAME="Sales"><GUID>L2</GUID><CLOSINGBALANCE>%s</CLOSINGBALANCE></LEDGER>`, amt(s))
	case strings.Contains(body, "GuruVchIDs"), strings.Contains(body, "GuruVouchers"):
		for _, v := range f.vs {
			if v.date < from || v.date > to {
				continue
			}
			fmt.Fprintf(&out, "<VOUCHER><DATE>%s</DATE><GUID>%s</GUID><ALTERID>1</ALTERID><VOUCHERTYPENAME>Sales</VOUCHERTYPENAME><VOUCHERNUMBER>%s</VOUCHERNUMBER>"+
				"<ALLLEDGERENTRIES.LIST><LEDGERNAME>Cash</LEDGERNAME><AMOUNT>-%s</AMOUNT></ALLLEDGERENTRIES.LIST>"+
				"<ALLLEDGERENTRIES.LIST><LEDGERNAME>Sales</LEDGERNAME><AMOUNT>%s</AMOUNT></ALLLEDGERENTRIES.LIST></VOUCHER>",
				v.date, v.guid, v.guid, amt(v.paise), amt(v.paise))
		}
	}
	out.WriteString("</COLLECTION></DATA></BODY></ENVELOPE>")
	w.Write([]byte(out.String()))
}

func d(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

func setup(t *testing.T, ft *fakeTally) (*sync.Engine, tbcheck.Result) {
	srv := httptest.NewServer(ft)
	t.Cleanup(srv.Close)
	c := tally.NewClient("x", 0)
	c.URL, c.Pause = srv.URL, 0
	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var s int64
	for _, v := range ft.vs {
		if !ft.skip[v.guid] {
			s += v.paise
		}
	}
	db.ReplaceLedgers([]tally.Ledger{{Name: "Cash", GUID: "L1", Closing: tally.Paise(-s)}, {Name: "Sales", GUID: "L2", Closing: tally.Paise(s)}})
	// Local copy: April right; May's voucher saved with a stale amount (100 instead of 300).
	tx, _ := db.BeginVouchers()
	for _, v := range ft.vs {
		p := v.paise
		if v.guid == "M1" {
			p = 10000
		}
		tx.Put(tally.Voucher{GUID: v.guid, Date: d(v.date[:4] + "-" + v.date[4:6] + "-" + v.date[6:]), Type: "Sales", Number: v.guid, AlterID: 1,
			Lines: []tally.LedgerLine{{Ledger: "Cash", Amount: tally.Paise(-p)}, {Ledger: "Sales", Amount: tally.Paise(p)}}})
	}
	tx.Commit()
	r, _ := tbcheck.Run(db)
	if r.OK() {
		t.Fatal("setup must start with a mismatch")
	}
	return &sync.Engine{C: c, DB: db}, r
}

func TestHealRereadsWrongMonth(t *testing.T) {
	ft := &fakeTally{vs: []fv{{"A1", "20260405", 10000}, {"M1", "20260510", 30000}, {"J1", "20260602", 5000}}}
	e, r := setup(t, ft)
	res, err := Run(context.Background(), e, r, d("2026-04-01"), d("2026-06-30"), "", func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TB.OK() || len(res.Refetched) != 1 || res.Refetched[0].From.Month() != time.May {
		t.Fatalf("expected May re-read and a match: %+v\n%s", res.Refetched, res.TB)
	}
}

func TestHealNamesVoucherTallyLeavesOut(t *testing.T) {
	ft := &fakeTally{vs: []fv{{"A1", "20260405", 10000}, {"M1", "20260510", 30000}, {"J1", "20260602", 7000}},
		skip: map[string]bool{"J1": true}}
	e, r := setup(t, ft)
	dir := t.TempDir()
	res, err := Run(context.Background(), e, r, d("2026-04-01"), d("2026-06-30"), dir, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if res.TB.OK() || len(res.Left) != 2 {
		t.Fatalf("expected June still differing for Cash and Sales: %+v", res.Left)
	}
	s := res.String()
	if !strings.Contains(s, "Jun 2026") || !strings.Contains(s, "no. J1") || res.Excel == "" {
		t.Fatalf("summary must name June, voucher J1 and the Excel file:\n%s", s)
	}
}
