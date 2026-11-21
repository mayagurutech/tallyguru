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
		runSync = syncjob.Run
		for i := 0; i < 200 && bg.busy(); i++ {
			time.Sleep(50 * time.Millisecond)
		}
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
	if !authorised(e, "TEST MCP") {
		t.Error("authorisation not recorded")
	}
	if c, _ := config.Load(e.Root); c.Agreed == "" {
		t.Error("disclaimer acceptance not recorded")
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
