package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/i18n"
	"github.com/mayagurutech/tallyguru/internal/mcp"
	"github.com/mayagurutech/tallyguru/internal/onboard"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// bgSync is the one background sync started by company_connect (first sync of a company can
// take minutes; the chat must not wait for it).
type bgSync struct {
	mu      sync.Mutex
	running bool
	company string
	last    string // latest progress line
	result  string // summary when done
	started time.Time
}

var bg bgSync

// runSync is syncjob.Run (swapped in tests).
var runSync = syncjob.Run

func (b *bgSync) view(company string) (running bool, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.company == "" || (company != "" && !strings.EqualFold(b.company, company)) {
		return false, ""
	}
	if b.running {
		return true, fmt.Sprintf("Sync of %s running for %s: %s", b.company, time.Since(b.started).Round(time.Second), b.last)
	}
	return false, b.result
}

func (b *bgSync) busy() bool { b.mu.Lock(); defer b.mu.Unlock(); return b.running }

func (e *Env) startBackgroundSync(company string) bool {
	bg.mu.Lock()
	if bg.running {
		bg.mu.Unlock()
		return false
	}
	bg.running, bg.company, bg.last, bg.result, bg.started = true, company, "starting", "", time.Now()
	bg.mu.Unlock()
	c, root := e.Tally, e.Root
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		res, err := runSync(ctx, c, syncjob.Job{Root: root, Company: company, Source: store.Live,
			Progress: func(s string) { bg.mu.Lock(); bg.last = strings.TrimSpace(s); bg.mu.Unlock() }})
		bg.mu.Lock()
		defer bg.mu.Unlock()
		bg.running = false
		switch {
		case err != nil && res.Mode == "":
			bg.result = fmt.Sprintf("Sync of %s stopped: %v", company, err)
		default:
			bg.result = fmt.Sprintf("Sync of %s finished in %s.\n%s", company, time.Since(bg.started).Round(time.Second), res)
		}
	}()
	return true
}

// authForm is what the AI app shows the user. The answer goes from the user to this server;
// the AI cannot fill it.
var authForm = json.RawMessage(`{"type":"object","properties":{` +
	`"kind":{"type":"string","title":"Whose books are these?","enum":["self","owner"],` +
	`"enumNames":["A client's books: I hold the business's written authorisation","My own business's books"]},` +
	`"typed":{"type":"string","title":"Type I AGREE to accept"}},"required":["kind","typed"]}`)

func (e *Env) companyConnect(a map[string]any) (string, error) {
	if e.Tally == nil {
		return "", fmt.Errorf("connecting to TallyPrime is not available here")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cs, err := onboard.OpenCompanies(ctx, e.Tally)
	if err != nil {
		return fmt.Sprintf("TallyPrime is not answering at %s. In TallyPrime press F1 (Help) > Settings > "+
			"Connectivity, set 'TallyPrime acts as' to Server (or Both) and Port to %d, restart TallyPrime if it asks, "+
			"open the company, then ask again.", e.Tally.URL, e.Cfg.Port), nil
	}
	if len(cs) == 0 {
		return "No company is open in TallyPrime. Open it there (Alt+F3), then ask again.", nil
	}
	co := pickCompany(cs, str(a, "company"))
	if co == nil {
		var b strings.Builder
		b.WriteString("Open in TallyPrime:\n")
		for _, c := range cs {
			fmt.Fprintf(&b, "- %s%s\n", c.Name, e.connectedNote(c))
		}
		b.WriteString("Say which one to connect.")
		return b.String(), nil
	}
	label := store.Label{Company: co.Name, GSTIN: co.GSTIN, Source: store.Live}
	if _, err := (auth.Store{Dir: label.Dir(e.Root), Box: e.Box}).Valid(co.Name, e.Now()); err != nil &&
		!onboard.HasAnnual(e.Root, e.Box, e.Now()) {
		if msg, err := e.askAuthorisation(ctx, *co); msg != "" || err != nil {
			return msg, err
		}
	}
	if running, text := bg.view(""); running {
		return text + "\nAsk again when it finishes.", nil
	}
	e.startBackgroundSync(co.Name)
	return fmt.Sprintf("%s is connected. Sync started in the background: month by month, TallyPrime stays usable. "+
		"A large company can take a few minutes the first time. Use sync_status to see progress.", co.Name), nil
}

// askAuthorisation records the yearly DPDP declaration (covers every company connected this
// financial year; author's choice, 21 Nov 2026) from the user's own typed answer. Each company
// gets its own record from it at its first sync. Returns a message when the flow stops here.
func (e *Env) askAuthorisation(ctx context.Context, co tally.Company) (string, error) {
	L := i18n.Of(e.Cfg.Language)
	self, owner := onboard.AnnualTexts(e.Cfg.Language, auth.Self), onboard.AnnualTexts(e.Cfg.Language, auth.Owner)
	var m strings.Builder
	needAgree := onboard.NeedAgree(e.Cfg)
	if needAgree {
		m.WriteString(i18n.T(L, "tool_disclaimer") + "\n\n")
	}
	fmt.Fprintf(&m, "Financial year %s: asked once a year, covers %s and every company you connect this year.\n\n%s\n\n%s\n%s\n\n%s\n%s\n\n%s", auth.FY(e.Now()), co.Name, self.Notice,
		self.Heading, self.Declaration, owner.Heading, owner.Declaration, self.Disclaimer)
	if e.Ask == nil {
		return e.panelInstead(co.Name)
	}
	ans, err := e.Ask(m.String(), authForm)
	if errors.Is(err, mcp.ErrNoElicit) {
		return e.panelInstead(co.Name)
	}
	if err != nil {
		return "", err
	}
	if ans.Action != "accept" {
		return "Not authorised, so nothing was read from TallyPrime. Ask again when you are ready.", nil
	}
	kind, _ := ans.Content["kind"].(string)
	typed, _ := ans.Content["typed"].(string)
	if kind != string(auth.Owner) {
		kind = string(auth.Self)
	}
	if err := onboard.RecordAnnual(e.Root, auth.Kind(kind), typed, e.Now()); err != nil {
		return "", err
	}
	if needAgree {
		if err := onboard.Agree(e.Root, e.Now()); err != nil {
			return "", err
		}
		if c, err := config.Load(e.Root); err == nil {
			e.Cfg.Agreed = c.Agreed
		}
	}
	return "", nil
}

func (e *Env) panelInstead(company string) (string, error) {
	if e.OpenPanel == nil {
		return "", fmt.Errorf("record the authorisation for %q in the control panel (Start menu: Tally MCP control panel), then ask again", company)
	}
	if err := e.OpenPanel(); err != nil {
		return "", fmt.Errorf("could not open the control panel (%v); open it from the Start menu: Tally MCP control panel", err)
	}
	return fmt.Sprintf("This AI app cannot show the authorisation form, so the control panel opened in your browser. "+
		"Under Add a company, pick %s, type I AGREE and click Record and continue. Then ask me again to connect it.", company), nil
}

// connectedNote says whether a company open in Tally already has local data.
func (e *Env) connectedNote(c tally.Company) string {
	l := store.Label{Company: c.Name, GSTIN: c.GSTIN, Source: store.Live}
	if _, err := (auth.Store{Dir: l.Dir(e.Root), Box: e.Box}).Valid(c.Name, e.Now()); err == nil {
		return " (connected)"
	}
	return ""
}

// pickCompany finds the company by exact name, then by a unique part of it; with one company
// open and no name given, that one.
func pickCompany(cs []tally.Company, want string) *tally.Company {
	if want == "" {
		if len(cs) == 1 {
			return &cs[0]
		}
		return nil
	}
	for i := range cs {
		if strings.EqualFold(cs[i].Name, want) {
			return &cs[i]
		}
	}
	var hit *tally.Company
	for i := range cs {
		if strings.Contains(strings.ToLower(cs[i].Name), strings.ToLower(want)) {
			if hit != nil {
				return nil
			}
			hit = &cs[i]
		}
	}
	return hit
}

// openInTally returns the companies open in TallyPrime now (cached for a minute; empty when
// Tally does not answer within 3 s).
func (e *Env) openInTally() []string {
	if e.Tally == nil {
		return nil
	}
	if time.Since(e.openAt) < time.Minute {
		return e.openNames
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cs, _ := onboard.OpenCompanies(ctx, e.Tally)
	e.openNames, e.openAt = nil, time.Now()
	for _, c := range cs {
		e.openNames = append(e.openNames, c.Name)
	}
	return e.openNames
}

// StartupNote tells the AI app, when it connects, which companies are open in TallyPrime and
// which are connected, so it can use them without being told. Empty when Tally is not open.
func (e *Env) StartupNote() string {
	names := e.openInTally()
	if len(names) == 0 {
		return ""
	}
	connected := map[string]bool{}
	if ls, err := store.List(e.Root); err == nil {
		for _, l := range ls {
			if l.Source == store.Live {
				connected[strings.ToLower(l.Company)] = true
			}
		}
	}
	var parts []string
	for _, n := range names {
		if connected[strings.ToLower(n)] {
			parts = append(parts, n+" (connected)")
		} else {
			parts = append(parts, n+" (not connected: sync_now connects it)")
		}
	}
	return "Open in TallyPrime now: " + strings.Join(parts, "; ") + ". Without a company name, use the one open in TallyPrime."
}
