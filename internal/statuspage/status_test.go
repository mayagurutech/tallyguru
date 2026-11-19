package statuspage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

func setup(t *testing.T) (string, config.Config) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(root, store.Label{Company: "TEST MCP", Source: store.Live})
	if err != nil {
		t.Fatal(err)
	}
	db.ReplaceLedgers([]tally.Ledger{{GUID: "1", Name: "Ram Traders", Parent: "Sundry Debtors", Opening: -987654}})
	db.SetMeta("tb_status", "ok")
	db.SetMeta("last_sync", "2026-11-14T19:33:00+05:30")
	db.Exec(`INSERT INTO ledger_map VALUES('1','flag','debtor','auto','','rule',''),('1','gst','eligible','ask','','rule','')`)
	db.Close()
	cfg := config.Default()
	cfg.ExportDir = t.TempDir()
	return root, cfg
}

func TestCollect(t *testing.T) {
	root, cfg := setup(t)
	// Tally not running on this port: must say so, not fail.
	s := Collect(context.Background(), root, "test", cfg, tally.NewClient("127.0.0.1", 1), time.Now())
	if s.Tally.Reachable || s.Tally.Error == "" {
		t.Errorf("conn %+v", s.Tally)
	}
	if len(s.Companies) != 1 {
		t.Fatalf("companies %+v", s.Companies)
	}
	c := s.Companies[0]
	if c.TB != "ok" || c.Ledgers != 1 || c.NeedUser != 1 || len(c.Mapping) != 2 || c.LastSync == "" {
		t.Errorf("%+v", c)
	}
	if c.AuthOK {
		t.Error("no authorisation recorded, must not show OK")
	}
	// Status carries counts only: no ledger names, no amounts.
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "Ram Traders") || strings.Contains(string(b), "9876") {
		t.Errorf("status leaks books data: %s", b)
	}
}

func TestHandlerNeedsKeyAndPost(t *testing.T) {
	root, cfg := setup(t)
	done := make(chan struct{})
	srv := httptest.NewServer(Handler(root, "test", cfg, nil, "k1", done, func(string) {}))
	defer srv.Close()
	get := func(p string) int {
		r, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, r.Body)
		r.Body.Close()
		return r.StatusCode
	}
	for _, p := range []string{"/", "/data", "/?t=bad", "/data?t=bad"} {
		if c := get(p); c != http.StatusForbidden {
			t.Errorf("%s without the key: %d", p, c)
		}
	}
	if c := get("/?t=k1"); c != 200 {
		t.Errorf("page with key: %d", c)
	}
	if c := get("/data?t=k1"); c != 200 {
		t.Errorf("data with key: %d", c)
	}
	if c := get("/finish?t=k1"); c == 200 {
		select {
		case <-done:
			t.Error("GET must not close the page")
		default:
		}
	}
	r, _ := http.Post(srv.URL+"/finish?t=k1", "", nil)
	r.Body.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("POST /finish did not close")
	}
}
