package statuspage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tools"
	"github.com/mayagurutech/tallyguru/internal/write"
)

// ReleasesURL is the public release info the update check reads (nothing is sent).
var ReleasesURL = "https://api.github.com/repos/mayagurutech/tallyguru/releases/latest"

// LiveWord must be typed to allow writes to a live company.
const LiveWord = "ALLOW WRITES"

func findLabel(root, company string, src store.Source) (store.Label, error) {
	ls, err := store.List(root)
	if err != nil {
		return store.Label{}, err
	}
	for _, l := range ls {
		if l.Company == company && (src == "" || l.Source == src) {
			return l, nil
		}
	}
	return store.Label{}, fmt.Errorf("%q is not synced", company)
}

// LogLine is one write from writes.jsonl, shortened for the panel.
type LogLine struct {
	Time, Action, What, GUID string
}

func readLog(dir string, n int) []LogLine {
	f, err := os.Open(filepath.Join(dir, "writes.jsonl"))
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []LogLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		l := LogLine{Time: fmt.Sprint(m["time"]), Action: fmt.Sprint(m["action"])}
		if g, ok := m["guid"].(string); ok {
			l.GUID = g
		}
		pick := func(x any) string {
			d, _ := x.(map[string]any)
			if d == nil {
				return ""
			}
			if k, ok := d["kind"].(string); ok {
				return k + " " + fmt.Sprint(d["name"])
			}
			return strings.TrimSpace(fmt.Sprint(d["type"]) + " " + fmt.Sprint(d["number"]) + " " + fmt.Sprint(d["date"]))
		}
		l.What = pick(m["draft"])
		if l.What == "" {
			l.What = pick(m["before"])
		}
		l.What = strings.ReplaceAll(l.What, "<nil>", "")
		all = append(all, l)
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all
}

func addWriteRoutes(mux *http.ServeMux, root, version string, ok, post func(*http.Request) bool) {
	deny := func(w http.ResponseWriter) { http.Error(w, "forbidden", http.StatusForbidden) }
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	q := func(r *http.Request) (string, store.Source) {
		return r.URL.Query().Get("company"), store.Source(r.URL.Query().Get("source"))
	}
	mux.HandleFunc("/drafts", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			deny(w)
			return
		}
		co, src := q(r)
		l, err := findLabel(root, co, src)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		db, err := store.Open(root, l)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer db.Close()
		items, err := write.Waiting(db.DB)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cfg, _ := config.Load(root)
		config.SetLiveWrites(cfg.LiveWrites)
		js(w, map[string]any{"Items": items, "Log": readLog(l.Dir(root), 30),
			"Writable": config.CheckWrite(co) == nil, "Test": strings.EqualFold(co, config.TestCompany)})
	})
	mux.HandleFunc("/act", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		var rq struct {
			Company, Source, Tool string
			Args                  map[string]any
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rq); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cfg, _ := config.Load(root)
		config.SetLiveWrites(cfg.LiveWrites)
		box, err := crypt.LoadOrCreate(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rq.Args == nil {
			rq.Args = map[string]any{}
		}
		rq.Args["company"] = rq.Company
		e := &tools.Env{Root: root, Cfg: cfg, Box: box, Now: time.Now, Tally: tally.NewClient(cfg.Host, cfg.Port)}
		out, err := e.Panel(rq.Tool, rq.Args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, out)
	})
	mux.HandleFunc("/livewrites", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		var rq struct {
			Company, Typed string
			On             bool
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&rq); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(rq.Company, config.TestCompany) {
			http.Error(w, "TEST MCP is always writable", http.StatusBadRequest)
			return
		}
		if rq.On && strings.TrimSpace(rq.Typed) != LiveWord {
			http.Error(w, "type "+LiveWord+" to allow writes to "+rq.Company, http.StatusBadRequest)
			return
		}
		cfg, err := config.Load(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var keep []string
		for _, n := range cfg.LiveWrites {
			if !strings.EqualFold(n, rq.Company) {
				keep = append(keep, n)
			}
		}
		if rq.On {
			keep = append(keep, rq.Company)
		}
		cfg.LiveWrites = keep
		if err := cfg.Save(root); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if rq.On {
			fmt.Fprintf(w, "Writes allowed for %s. Take a Tally backup (Alt+Y) before posting. Restart your AI app for it to apply there.", rq.Company)
		} else {
			fmt.Fprintf(w, "Writes switched off for %s.", rq.Company)
		}
	})
	mux.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ReleasesURL, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "tally-mcp-by-guru") // generic client name, no user details
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			fmt.Fprintf(w, "Could not reach GitHub (%v). You are on %s.", err, version)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			fmt.Fprintf(w, "No public release yet. You are on %s.", version)
			return
		}
		var rel struct {
			Tag  string `json:"tag_name"`
			URL  string `json:"html_url"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, resp.Body, 1<<20)).Decode(&rel); err != nil || rel.Tag == "" {
			fmt.Fprintf(w, "Could not read the release info. You are on %s.", version)
			return
		}
		if strings.TrimPrefix(rel.Tag, "v") == strings.TrimPrefix(version, "v") {
			fmt.Fprintf(w, "You have the latest version (%s).", version)
			return
		}
		fmt.Fprintf(w, "Version %s is available (you have %s). Download it from %s. Nothing is downloaded automatically.", rel.Tag, version, rel.URL)
	})
}
