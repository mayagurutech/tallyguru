// Package statuspage serves a local status page: connection, companies, sync and trial
// balance check, mapping progress, authorisation, recent exports and switched-on features.
// 127.0.0.1 only, a one-time key in the address, no external scripts, nothing sent anywhere,
// no writes to the books. It runs only while the user keeps it open.
package statuspage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
	"github.com/mayagurutech/tallyguru/internal/tools"
)

// Status is everything the page shows. Counts and dates only: no amounts, no party names.
type Status struct {
	Version   string
	Tally     Conn
	ReadOnly  bool
	Language  string
	Modules   []Module
	Companies []Company
	ExportDir string
	Exports   []Export
	Checked   string
}

// Conn is the Tally connection.
type Conn struct {
	URL       string
	Reachable bool
	Error     string
	Open      []string // companies open in TallyPrime now
}

// Module is one feature and whether it is on.
type Module struct {
	ID, Name string
	On       bool
	Built    bool // available in this version
}

// Company is one synced company.
type Company struct {
	Name, GSTIN, Source string
	LastSync, TB        string
	TBDetail            string
	Vouchers, Ledgers   int
	From, To            string
	Mapping             []Fam
	NeedUser            int
	Auth                string // e.g. "Owner's declaration, FY 2026-27, accepted 14-11-2026"
	AuthOK              bool
	AuthDue             string // next re-confirmation
}

// Fam is one mapping family's progress.
type Fam struct {
	Family                 string
	User, Auto, Ask, Total int
}

// Export is a recent Excel file.
type Export struct {
	Company, Name, When string
	Size                int64
}

// Collect gathers the status. It may contact Tally once (a ping), nothing else.
func Collect(ctx context.Context, root, version string, cfg config.Config, c *tally.Client, now time.Time) Status {
	s := Status{Version: version, ReadOnly: cfg.ReadOnly, Language: cfg.Language,
		ExportDir: cfg.ExportBase(), Checked: now.Format("02-01-2006 15:04:05")}
	for _, m := range config.Modules {
		s.Modules = append(s.Modules, Module{m.ID, m.Name, cfg.Modules[m.ID], m.Phase <= config.CurrentPhase})
	}
	if c != nil {
		s.Tally.URL = c.URL
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if err := c.Ping(pctx); err != nil {
			s.Tally.Error = "not reachable: is TallyPrime open with the XML port on?"
		} else {
			s.Tally.Reachable = true
			if cs, err := openCompanies(pctx, c); err == nil {
				s.Tally.Open = cs
			}
		}
		cancel()
	}
	box, _ := crypt.LoadOrCreate(root)
	ls, _ := store.List(root)
	for _, l := range ls {
		s.Companies = append(s.Companies, company(root, l, box, now))
	}
	s.Exports = recentExports(s.ExportDir, 10)
	return s
}

func openCompanies(ctx context.Context, c *tally.Client) ([]string, error) {
	var out []string
	err := c.Do(ctx, tally.CompanyListRequest().Build(), func(r io.Reader) error {
		return tally.ParseCompanies(r, func(co tally.Company) error { out = append(out, co.Name); return nil })
	})
	return out, err
}

func company(root string, l store.Label, box *crypt.Box, now time.Time) Company {
	c := Company{Name: l.Company, GSTIN: l.GSTIN, Source: string(l.Source)}
	if box != nil {
		if rec, err := (auth.Store{Dir: l.Dir(root), Box: box}).Valid(l.Company, now); err == nil {
			c.AuthOK = true
			c.Auth = kindName[rec.Kind] + ", FY " + rec.FY + ", accepted " + rec.AcceptedAt.Format("02-01-2006")
			if len(rec.FY) == 7 { // "2026-27": re-confirm on 1 April 2027
				c.AuthDue = "01-04-" + rec.FY[:2] + rec.FY[5:]
			}
		} else {
			c.Auth = err.Error()
		}
	}
	db, err := store.Open(root, l)
	if err != nil {
		c.TB = "cannot open: " + err.Error()
		return c
	}
	defer db.Close()
	last, _ := db.Meta("last_sync")
	if t, err := time.Parse(time.RFC3339, last); err == nil {
		c.LastSync = t.Format("02-01-2006 15:04")
	}
	c.TB, _ = db.Meta("tb_status")
	if c.TB != "ok" {
		c.TBDetail, _ = db.Meta("tb_detail")
	}
	var from, to string
	db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(date),''), COALESCE(MAX(date),'') FROM vouchers`).Scan(&c.Vouchers, &from, &to)
	db.QueryRow(`SELECT COUNT(*) FROM ledgers`).Scan(&c.Ledgers)
	c.From, c.To = ddmmyyyy(from), ddmmyyyy(to)
	rows, err := db.Query(`SELECT family, SUM(confirmed<>''), SUM(confirmed='' AND conf<>'ask'), SUM(confirmed='' AND conf='ask'), COUNT(*)
		FROM ledger_map GROUP BY family ORDER BY family`)
	if err == nil {
		for rows.Next() {
			var f Fam
			rows.Scan(&f.Family, &f.User, &f.Auto, &f.Ask, &f.Total)
			c.NeedUser += f.Ask
			c.Mapping = append(c.Mapping, f)
		}
		rows.Close()
	}
	return c
}

var kindName = map[auth.Kind]string{auth.Self: "Self-certification", auth.Owner: "Owner's declaration",
	auth.Letter: "Signed letter on file", auth.Email: "Email on file"}

func ddmmyyyy(d string) string {
	t, err := time.Parse(time.DateOnly, d)
	if err != nil {
		return ""
	}
	return t.Format("02-01-2006")
}

func recentExports(base string, n int) []Export {
	var out []Export
	dirs, _ := os.ReadDir(base)
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(base, d.Name()))
		for _, f := range files {
			if !strings.HasSuffix(strings.ToLower(f.Name()), ".xlsx") {
				continue
			}
			if fi, err := f.Info(); err == nil {
				out = append(out, Export{d.Name(), f.Name(), fi.ModTime().Format("02-01-2006 15:04"), fi.Size()})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := time.Parse("02-01-2006 15:04", out[i].When)
		b, _ := time.Parse("02-01-2006 15:04", out[j].When)
		return a.After(b)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// exportFolder is the folder of one company's exports (for "open folder").
func exportFolder(cfg config.Config, company string) string {
	return filepath.Join(cfg.ExportBase(), tools.SafeName(company))
}
