package statuspage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// addCompanyRoutes adds the Companies and AI app actions to the panel.
func addCompanyRoutes(mux *http.ServeMux, root string, ok, post func(*http.Request) bool) {
	jobs := &Jobs{}
	client := func() *tally.Client { c, _ := config.Load(root); return tally.NewClient(c.Host, c.Port) }
	js := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	body := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(v); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return false
		}
		return true
	}
	deny := func(w http.ResponseWriter) { http.Error(w, "forbidden", http.StatusForbidden) }

	mux.HandleFunc("/open", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			deny(w)
			return
		}
		cs, err := OpenCompanies(r.Context(), client())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		var names []string
		for _, c := range cs {
			names = append(names, c.Name)
		}
		js(w, names)
	})
	mux.HandleFunc("/authtext", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			deny(w)
			return
		}
		c, _ := config.Load(root)
		js(w, AuthTexts(c.Language, auth.Kind(r.URL.Query().Get("kind"))))
	})
	mux.HandleFunc("/authorise", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		var rq AuthRequest
		if !body(w, r, &rq) {
			return
		}
		if err := Authorise(r.Context(), root, client(), rq, time.Now()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "Authorisation recorded for %s, FY %s. You can sync it now.", rq.Company, auth.FY(time.Now()))
	})
	mux.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		var rq struct {
			Company, Source string
			Full            bool
		}
		if !body(w, r, &rq) {
			return
		}
		src := store.Source(rq.Source)
		if src == "" {
			src = store.Live
		}
		if err := jobs.Start(root, client(), rq.Company, src, rq.Full); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "started")
	})
	mux.HandleFunc("/job", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			deny(w)
			return
		}
		js(w, jobs.View())
	})
	mux.HandleFunc("/forget", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		var rq struct{ Company, Source, Typed string }
		if !body(w, r, &rq) {
			return
		}
		if v := jobs.View(); v.Running && v.Company == rq.Company {
			http.Error(w, "wait for the sync to finish", http.StatusConflict)
			return
		}
		dir, err := Forget(root, rq.Company, store.Source(rq.Source), rq.Typed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "Deleted all local data for %s (%s). Nothing in Tally was changed.", rq.Company, dir)
	})
	mux.HandleFunc("/claude", func(w http.ResponseWriter, r *http.Request) {
		if !ok(r) {
			deny(w)
			return
		}
		var p ClaudePlan
		var err error
		if r.Method == http.MethodPost {
			p, err = ConnectClaude(root)
		} else {
			p, err = PlanClaude(root)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		js(w, p)
	})
}
