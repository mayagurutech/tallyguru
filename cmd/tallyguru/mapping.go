package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/auth"
	"github.com/mayagurutech/tallyguru/internal/crypt"
	"github.com/mayagurutech/tallyguru/internal/mapping"
	"github.com/mayagurutech/tallyguru/internal/review"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// runMapping: mapping status|export|import "<company>" [file.csv]. The CSV opens in Excel
// and keeps real names; it stays on this PC. On import, rows with confirm = yes are saved.
func runMapping(root string, args []string, src store.Source) error {
	if len(args) < 2 {
		return fmt.Errorf(`usage: tallyguru mapping review|status|export|import "<company>" [file.csv]`)
	}
	cmd, name := args[0], strings.TrimSpace(args[1])
	var l *store.Label
	ls, err := store.List(root)
	if err != nil {
		return err
	}
	for i := range ls {
		if strings.EqualFold(ls[i].Company, name) && ls[i].Source == src {
			l = &ls[i]
		}
	}
	if l == nil {
		return fmt.Errorf("%q (%s) is not synced yet", name, src)
	}
	box, err := crypt.LoadOrCreate(root)
	if err != nil {
		return err
	}
	if _, err := (auth.Store{Dir: l.Dir(root), Box: box}).Valid(l.Company, time.Now()); err != nil {
		return err
	}
	db, err := store.Open(root, *l)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.RefreshMapping(); err != nil {
		return err
	}
	file := l.Company + " mapping.csv"
	if len(args) > 2 {
		file = args[2]
	}
	switch cmd {
	case "status":
		rs, err := db.MapRows("", false)
		if err != nil {
			return err
		}
		n := map[string]int{}
		for _, r := range rs {
			if r.Confirmed != "" {
				n["confirmed"]++
			} else {
				n[r.Conf]++
			}
		}
		fmt.Printf("%s: %d tags, %d confirmed; pending high %d, medium %d, low %d\n",
			l.Company, len(rs), n["confirmed"], n[mapping.High], n[mapping.Medium], n[mapping.Low])
	case "review":
		return review.Serve(db, func(s string) { fmt.Println(s) })
	case "export":
		rs, err := db.MapRows("", false)
		if err != nil {
			return err
		}
		f, err := os.Create(file)
		if err != nil {
			return err
		}
		f.WriteString("\xef\xbb\xbf") // so Excel reads UTF-8
		w := csv.NewWriter(f)
		w.Write([]string{"ledger", "group", "family", "tag", "confidence", "reason", "confirm"})
		for _, r := range rs {
			c := ""
			if r.Confirmed != "" {
				c = "yes"
			}
			w.Write([]string{r.Name, r.Parent, r.Family, r.Tag, r.Conf, r.Reason, c})
		}
		w.Flush()
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Printf("Wrote %d rows to %s. Edit 'tag' where needed, put 'yes' in 'confirm', then: tallyguru mapping import %q %q\n", len(rs), file, l.Company, file)
	case "import":
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		rd := csv.NewReader(f)
		rd.FieldsPerRecord = -1
		recs, err := rd.ReadAll()
		if err != nil {
			return err
		}
		var ok, skip int
		var bad []string
		for i, r := range recs {
			if i == 0 || len(r) < 7 || !strings.EqualFold(strings.TrimSpace(r[6]), "yes") {
				continue
			}
			ledger := strings.TrimPrefix(r[0], "\xef\xbb\xbf")
			tag, valid := mapping.Valid(r[2], r[3])
			if !valid {
				bad = append(bad, fmt.Sprintf("row %d: %q is not a %s tag", i+1, r[3], r[2]))
				continue
			}
			done, err := db.SetTag(ledger, r[2], tag, "user", time.Now())
			if err != nil {
				return err
			}
			if done {
				ok++
			} else {
				skip++
			}
		}
		fmt.Printf("Confirmed %d tags; %d rows skipped (ledger gone or family not applicable).\n", ok, skip)
		for _, b := range bad {
			fmt.Println("  " + b)
		}
	default:
		return fmt.Errorf("unknown mapping command %q", cmd)
	}
	return nil
}
