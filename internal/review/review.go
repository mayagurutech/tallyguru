// Package review serves a one-page mapping review on 127.0.0.1 only. Nothing is sent
// anywhere else; the page has no external scripts. A random token in the URL stops other
// web pages in the browser from reading or saving.
package review

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/mapping"
	"github.com/mayagurutech/tallyguru/internal/store"
)

//go:embed page.html
var page string

type row struct {
	GUID, Ledger, Group, Family, Tag, Conf, Why string
	Done                                        bool
}

type fam struct {
	ID, Question, Help string
	Choices            [][2]string // tag, label
}

// Serve opens the review page in the browser and blocks until the user clicks Finish
// (or the process is stopped).
func Serve(db *store.DB, out func(string)) error {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	done := make(chan struct{})
	ok := func(r *http.Request) bool { return r.URL.Query().Get("t") == tok }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline'")
		fmt.Fprint(w, strings.Replace(page, "{{COMPANY}}", htmlEsc(db.Label.Company), 1))
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			http.Error(w, "forbidden", 403)
			return
		}
		rs, err := db.MapRows("", false)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		var out []row
		for _, m := range rs {
			out = append(out, row{m.GUID, m.Name, m.Parent, m.Family, m.Tag, m.Conf, m.Reason, m.Confirmed != ""})
		}
		var fs []fam
		for _, f := range []string{mapping.TDS, mapping.GST, mapping.Tax, mapping.Flag} {
			x := fam{ID: f, Question: mapping.Question[f], Help: mapping.Help[f]}
			for _, t := range mapping.Tags[f] {
				x.Choices = append(x.Choices, [2]string{t, mapping.Label[t]})
			}
			fs = append(fs, x)
		}
		json.NewEncoder(w).Encode(map[string]any{"rows": out, "families": fs})
	})
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) || r.Method != http.MethodPost {
			http.Error(w, "forbidden", 403)
			return
		}
		var in []struct{ GUID, Family, Tag string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		n := 0
		for _, x := range in {
			t, valid := mapping.Valid(x.Family, x.Tag)
			if !valid {
				continue
			}
			if done, err := db.SetTagGUID(x.GUID, x.Family, t, "user", time.Now()); err == nil && done {
				n++
			}
		}
		out(fmt.Sprintf("Saved %d answers.", n))
		json.NewEncoder(w).Encode(map[string]int{"saved": n})
	})
	mux.HandleFunc("/finish", func(w http.ResponseWriter, r *http.Request) {
		if ok(r) && r.Method == http.MethodPost {
			fmt.Fprint(w, "ok")
			close(done)
		}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr(), tok)
	out("Review page: " + url + "\nIt opens in your browser. Click Finish there when done (or press Ctrl+C here).")
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	<-done
	return srv.Close()
}

func htmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
