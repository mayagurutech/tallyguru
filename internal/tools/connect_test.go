package tools

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/onboard"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

const fakeCompanies = `<ENVELOPE><BODY><DATA><COLLECTION><COMPANY NAME="TEST MCP"><GUID>c1</GUID><BOOKSFROM>20250401</BOOKSFROM></COMPANY>` +
	`<COMPANY NAME="Other Co"><GUID>c2</GUID><BOOKSFROM>20250401</BOOKSFROM></COMPANY></COLLECTION></DATA></BODY></ENVELOPE>`

// fakeTally answers the company list; anything else gets an empty collection.
func fakeTally(t *testing.T) *tally.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "Company") && !strings.Contains(string(b), "Ledger") {
			io.WriteString(w, fakeCompanies)
			return
		}
		io.WriteString(w, `<ENVELOPE><BODY><DATA><COLLECTION> </COLLECTION></DATA></BODY></ENVELOPE>`)
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	return tally.NewClient(host, p)
}

func connectEnv(t *testing.T, c *tally.Client) *Env {
	root := t.TempDir()
	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		t.Fatal(err)
	}
	runSync = func(context.Context, *tally.Client, syncjob.Job) (syncjob.Result, error) {
		return syncjob.Result{Mode: "full"}, nil
	}
	t.Cleanup(func() { // let a background sync finish before the folder is removed
		for i := 0; i < 200 && bg.busy(); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		runSync = syncjob.Run
		bg = bgSync{}
	})
	return &Env{Root: root, Cfg: config.Default(), Box: box, Now: time.Now, Tally: c}
}

func authorised(e *Env, company string) bool {
	_, err := (auth.Store{Dir: store.Label{Company: company, Source: store.Live}.Dir(e.Root), Box: e.Box}).Valid(company, e.Now())
	return err == nil
}

func TestConnectTallyDown(t *testing.T) {
	e := connectEnv(t, tally.NewClient("127.0.0.1", 1))
	s, err := e.companyConnect(map[string]any{})
	if err != nil || !strings.Contains(s, "Connectivity") {
		t.Fatalf("want the Tally setting steps, got %q %v", s, err)
	}
}

func TestConnectListsThenAsksUser(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	s, _ := e.companyConnect(map[string]any{})
	if !strings.Contains(s, "TEST MCP") || !strings.Contains(s, "Other Co") || !strings.Contains(s, "Say which") {
		t.Fatalf("list: %q", s)
	}
	var asked string
	e.Ask = func(msg string, schema json.RawMessage) (mcp.Elicitation, error) {
		asked = msg
		return mcp.Elicitation{Action: "accept", Content: map[string]any{"kind": "self", "typed": "I AGREE"}}, nil
	}
	s, err := e.syncNow(map[string]any{"company": "test"})
	if err != nil || !strings.Contains(s, "Sync started") {
		t.Fatalf("connect: %q %v", s, err)
	}
	if !strings.Contains(asked, "Digital Personal Data Protection Act") || !strings.Contains(asked, "TEST MCP") {
		t.Errorf("the user must see the notice: %q", asked)
	}
	if !onboard.HasAnnual(e.Root, e.Box, e.Now()) || !strings.Contains(asked, "every company") {
		t.Errorf("the yearly declaration must be asked and recorded: %q", asked)
	}
	if c, _ := config.Load(e.Root); c.Agreed == "" {
		t.Error("disclaimer acceptance not recorded")
	}
	// A second company this year: no second question.
	for bg.busy() {
		time.Sleep(10 * time.Millisecond)
	}
	asked = ""
	if s, err := e.syncNow(map[string]any{"company": "Other Co"}); err != nil || !strings.Contains(s, "Sync started") || asked != "" {
		t.Fatalf("second company: %q %v asked=%q", s, err, asked)
	}
}

func TestConnectDeclinedOrWrongWords(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	e.Ask = func(string, json.RawMessage) (mcp.Elicitation, error) { return mcp.Elicitation{Action: "decline"}, nil }
	if s, _ := e.companyConnect(map[string]any{"company": "TEST MCP"}); !strings.Contains(s, "Not authorised") || authorised(e, "TEST MCP") {
		t.Fatalf("decline: %q", s)
	}
	e.Ask = func(string, json.RawMessage) (mcp.Elicitation, error) {
		return mcp.Elicitation{Action: "accept", Content: map[string]any{"kind": "self", "typed": "ok"}}, nil
	}
	if _, err := e.companyConnect(map[string]any{"company": "TEST MCP"}); err == nil || authorised(e, "TEST MCP") {
		t.Fatal("anything but the typed words must be refused")
	}
	// The AI cannot pass the typed words itself: the tool ignores any such argument.
	e.Ask = nil
	opened := false
	e.OpenPanel = func() error { opened = true; return nil }
	s, _ := e.companyConnect(map[string]any{"company": "TEST MCP", "typed": "I AGREE"})
	if !opened || !strings.Contains(s, "control panel") || authorised(e, "TEST MCP") {
		t.Fatalf("without a form the panel opens, nothing is recorded: %q", s)
	}
}

func TestConnectNoElicitOpensPanel(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	e.Ask = func(string, json.RawMessage) (mcp.Elicitation, error) { return mcp.Elicitation{}, mcp.ErrNoElicit }
	opened := false
	e.OpenPanel = func() error { opened = true; return nil }
	if s, _ := e.companyConnect(map[string]any{"company": "Other"}); !opened || !strings.Contains(s, "Other Co") {
		t.Fatalf("got %q", s)
	}
}

func TestStartupNote(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	st, _ := store.Open(e.Root, store.Label{Company: "TEST MCP", Source: store.Live})
	st.Close()
	n := e.StartupNote()
	if !strings.Contains(n, "TEST MCP (connected)") || !strings.Contains(n, "Other Co (not connected: sync_now connects it)") {
		t.Fatalf("note: %q", n)
	}
	if (&Env{Root: e.Root, Tally: tally.NewClient("127.0.0.1", 1)}).StartupNote() != "" {
		t.Error("Tally closed: no note")
	}
}

// A long sync (first sync of many years) must not hold up sync_now: the server answers one
// call at a time, so a foreground sync made that call and every later one time out.
func TestSyncNowLongSyncRunsInBackground(t *testing.T) {
	e := connectEnv(t, fakeTally(t))
	e.Ask = func(string, json.RawMessage) (mcp.Elicitation, error) {
		return mcp.Elicitation{Action: "accept", Content: map[string]any{"kind": "self", "typed": "I AGREE"}}, nil
	}
	if s, err := e.syncNow(map[string]any{"company": "TEST MCP"}); err != nil || !strings.Contains(s, "Sync started") {
		t.Fatalf("connect: %q %v", s, err)
	}
	bg.wait(5 * time.Second)
	l := store.Label{Company: "TEST MCP", Source: store.Live}
	st, _ := store.Open(e.Root, l)
	st.Close()
	if _, err := auth.CoverFromAnnual(auth.Store{Dir: e.Root, Box: e.Box}, auth.Store{Dir: l.Dir(e.Root), Box: e.Box},
		l.Company, "", string(l.Source), e.Now()); err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	runSync = func(context.Context, *tally.Client, syncjob.Job) (syncjob.Result, error) {
		<-release
		return syncjob.Result{Mode: "full"}, nil
	}
	old := syncWait
	syncWait = 100 * time.Millisecond
	defer func() { syncWait = old }()
	t0 := time.Now()
	s, err := e.syncNow(map[string]any{"company": "TEST MCP"})
	if err != nil || !strings.Contains(s, "running in the background") || time.Since(t0) > 2*time.Second {
		t.Fatalf("sync_now must answer quickly: %q %v after %s", s, err, time.Since(t0))
	}
	if s, _ := e.syncStatus(map[string]any{"company": "TEST MCP"}); !strings.Contains(s, "running for") {
		t.Fatalf("status while running: %q", s)
	}
	close(release)
	bg.wait(5 * time.Second)
	if _, s := bg.view("TEST MCP"); !strings.Contains(s, "finished") {
		t.Fatalf("result: %q", s)
	}
}
