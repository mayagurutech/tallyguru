package sync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	gosync "sync"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

type fv struct {
	guid, date string
	alter      int64
	amt        string
}

// fakeTally answers our three collection requests from an in-memory voucher list.
type fakeTally struct {
	mu   gosync.Mutex
	vs   []fv
	reqs []string
}

func (f *fakeTally) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	f.mu.Lock()
	defer f.mu.Unlock()
	from := tdate(between(body, `<SVFROMDATE TYPE="Date">`, "<"))
	to := tdate(between(body, `<SVTODATE TYPE="Date">`, "<"))
	var out strings.Builder
	out.WriteString("<ENVELOPE><BODY><DESC><CMPINFO><VOUCHER>0</VOUCHER></CMPINFO></DESC><DATA><COLLECTION>")
	switch {
	case strings.Contains(body, "GuruLedgers"):
		f.reqs = append(f.reqs, "ledgers")
		out.WriteString(`<LEDGER NAME="Cash"><GUID>L1</GUID><PARENT>Cash-in-Hand</PARENT><CLOSINGBALANCE>0</CLOSINGBALANCE></LEDGER>`)
		out.WriteString(`<LEDGER NAME="Sales"><GUID>L2</GUID><PARENT>Sales Accounts</PARENT></LEDGER>`)
	case strings.Contains(body, "GuruVchIDs"), strings.Contains(body, "GuruVouchers"):
		full := strings.Contains(body, "GuruVouchers")
		f.reqs = append(f.reqs, map[bool]string{true: "full", false: "ids"}[full]+" "+from)
		for _, v := range f.vs {
			if v.date < from || v.date > to {
				continue
			}
			fmt.Fprintf(&out, "<VOUCHER><DATE>%s</DATE><GUID>%s</GUID><ALTERID>%d</ALTERID>", v.date, v.guid, v.alter)
			if full {
				fmt.Fprintf(&out, "<VOUCHERTYPENAME>Sales</VOUCHERTYPENAME><ALLLEDGERENTRIES.LIST><LEDGERNAME>Cash</LEDGERNAME><AMOUNT>-%s</AMOUNT></ALLLEDGERENTRIES.LIST>"+
					"<ALLLEDGERENTRIES.LIST><LEDGERNAME>Sales</LEDGERNAME><AMOUNT>%s</AMOUNT></ALLLEDGERENTRIES.LIST>", v.amt, v.amt)
			}
			out.WriteString("</VOUCHER>")
		}
	}
	out.WriteString("</COLLECTION></DATA></BODY></ENVELOPE>")
	w.Write([]byte(out.String()))
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	return s[:j]
}

func TestIncrementalSync(t *testing.T) {
	ft := &fakeTally{vs: []fv{
		{"A", "20260405", 10, "100.00"},
		{"B", "20260410", 11, "200.00"},
		{"C", "20260502", 12, "300.00"},
	}}
	srv := httptest.NewServer(ft)
	defer srv.Close()
	c := tally.NewClient("x", 0)
	c.URL, c.Pause = srv.URL, 0

	db, err := store.Open(t.TempDir(), store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	e := &Engine{C: c, DB: db}
	o := Options{From: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)}

	st, err := e.Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fetched != 3 || st.Chunks != 2 || st.Ledgers != 2 {
		t.Fatalf("first sync: %v", st)
	}
	total := func() (n int, sum int64) {
		db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN amount>0 THEN amount END),0) FROM vouchers JOIN vlines ON guid=voucher_guid`).Scan(&n, &sum)
		return
	}
	if n, s := total(); n != 6 || s != 60000 {
		t.Fatalf("after first sync lines=%d sum=%d", n, s)
	}

	// Second sync with no change: only ID lists, no full fetch.
	ft.reqs = nil
	st, _ = e.Run(context.Background(), o)
	if st.Fetched != 0 || strings.Contains(strings.Join(ft.reqs, ","), "full") {
		t.Fatalf("unchanged sync fetched data: %v %v", st, ft.reqs)
	}

	// Alter B, delete C, add D in May.
	ft.mu.Lock()
	ft.vs = []fv{{"A", "20260405", 10, "100.00"}, {"B", "20260410", 20, "250.00"}, {"D", "20260520", 21, "50.00"}}
	ft.mu.Unlock()
	st, err = e.Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fetched != 2 || st.Deleted != 1 {
		t.Fatalf("incremental: %v", st)
	}
	if n, s := total(); n != 6 || s != 40000 {
		t.Fatalf("after incremental lines=%d sum=%d (want 6, 40000)", n, s)
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM vlines WHERE voucher_guid='C'`).Scan(&cnt)
	if cnt != 0 {
		t.Fatal("lines of deleted voucher left behind")
	}
}

func TestStoreRefusesOtherCompanyInSameFolder(t *testing.T) {
	root := t.TempDir()
	db, _ := store.Open(root, store.Label{Company: "TEST MCP", Source: store.Live})
	db.Close()
	l := store.Label{Company: "TEST MCP", Source: store.Backup}
	if l.Dir(root) == (store.Label{Company: "TEST MCP", Source: store.Live}).Dir(root) {
		t.Fatal("live and backup must use different folders")
	}
}

// tdate converts Tally's "1-Apr-2026" to "20260401" for comparisons in the fake.
func tdate(s string) string {
	t, err := time.Parse("2-Jan-2006", s)
	if err != nil {
		return ""
	}
	return t.Format("20060102")
}
