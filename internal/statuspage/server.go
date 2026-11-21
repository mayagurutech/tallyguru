package statuspage

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/review"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

//go:embed page.html
var page string

// Handler builds the page's routes. tok is the one-time key; done is closed on Finish.
func Handler(root, version string, cfg config.Config, c *tally.Client, tok string, done chan struct{}, out func(string)) http.Handler {
	ok := func(r *http.Request) bool { return r.URL.Query().Get("t") == tok }
	post := func(r *http.Request) bool { return ok(r) && r.Method == http.MethodPost }
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) || r.URL.Path != "/" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self' 'unsafe-inline'")
		fmt.Fprint(w, page)
	})
	mux.HandleFunc("/data", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		cur, _ := config.Load(root) // settings may have changed in this page
		b, err := json.Marshal(Collect(r.Context(), root, version, cur, tally.NewClient(cur.Host, cur.Port), time.Now()))
		if err != nil {
			http.Error(w, "status: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(b)
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s, err := LoadSettings(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s)
	})
	mux.HandleFunc("/agree", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := Agree(root, time.Now()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var ch Change
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&ch); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if _, err := Apply(root, ch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "Saved. Restart your AI app (fully quit Claude Desktop) to load the new features.")
	})
	mux.HandleFunc("/checks", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		cur, _ := config.Load(root)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Checks(r.Context(), root, cur, tally.NewClient(cur.Host, cur.Port)))
	})
	// Opens the existing mapping review page for one company (needs a valid authorisation).
	mux.HandleFunc("/review", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := openReview(root, r.URL.Query().Get("company"), store.Source(r.URL.Query().Get("source")), out); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/folder", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		d := cfg.ExportBase()
		if co := r.URL.Query().Get("company"); co != "" {
			d = exportFolder(cfg, co)
		}
		if _, err := os.Stat(d); err != nil {
			http.Error(w, "no exports yet", http.StatusNotFound)
			return
		}
		exec.Command("explorer", d).Start()
		fmt.Fprint(w, "ok")
	})
	addCompanyRoutes(mux, root, ok, post)
	addWriteRoutes(mux, root, version, ok, post)
	addUpdateRoutes(mux, version, post, func() {
		select {
		case <-done:
		default:
			close(done)
		}
	})
	mux.HandleFunc("/finish", func(w http.ResponseWriter, r *http.Request) {
		if post(r) {
			fmt.Fprint(w, "ok")
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	return mux
}

func openReview(root, company string, src store.Source, out func(string)) error {
	ls, err := store.List(root)
	if err != nil {
		return err
	}
	for _, l := range ls {
		if !strings.EqualFold(l.Company, company) || l.Source != src {
			continue
		}
		box, err := crypt.LoadOrCreate(root)
		if err != nil {
			return err
		}
		if _, err := (auth.Store{Dir: l.Dir(root), Box: box}).Valid(l.Company, time.Now()); err != nil {
			return err
		}
		db, err := store.Open(root, l)
		if err != nil {
			return err
		}
		if err := db.RefreshMapping(); err != nil {
			db.Close()
			return err
		}
		go func() { defer db.Close(); review.Serve(db, out) }()
		return nil
	}
	return fmt.Errorf("%q is not synced", company)
}

// IdleStop is how long the panel waits with no page open (the page pings every minute)
// before it stops by itself, so a panel started without a window never lingers.
var IdleStop = 30 * time.Minute

// Serve opens the status page in the browser and blocks until Finish, Ctrl+C, or IdleStop
// with no request from the page.
func Serve(root, version string, cfg config.Config, c *tally.Client, out func(string)) error {
	b := make([]byte, 16)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	done := make(chan struct{})
	var seen atomic.Int64
	seen.Store(time.Now().UnixNano())
	h := Handler(root, version, cfg, c, tok, done, out)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen.Store(time.Now().UnixNano())
			if r.URL.Path == "/ping" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.ServeHTTP(w, r)
		})}
	go srv.Serve(ln)
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if time.Since(time.Unix(0, seen.Load())) > IdleStop {
					close(stop)
					return
				}
			}
		}
	}()
	url := fmt.Sprintf("http://%s/?t=%s", ln.Addr(), tok)
	out("Status page: " + url + "\nIt opens in your browser. The panel stops by itself when no page has been open for a while.")
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	select {
	case <-done:
	case <-stop:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
