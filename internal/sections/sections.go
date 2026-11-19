// Package sections cites income-tax sections the way the author asked: the Income-tax Act,
// 2025 section first, the 1961 section in brackets. The pairs come from one table,
// sections_2025.csv (built in), which the user can override by placing a corrected copy in
// the data folder. Rows not yet checked against the Act print "(verify)".
package sections

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed sections_2025.csv
var builtIn []byte

// FileName is the override file name in the data folder.
const FileName = "sections_2025.csv"

// Row is one section pair.
type Row struct {
	Key, S2025, S1961, Subject, Source string
	Verified                           bool
}

var (
	mu    sync.Mutex
	table map[string]Row
)

// Load reads the table: the user's copy in root if present, else the built-in one.
func Load(root string) error {
	b := builtIn
	if root != "" {
		if x, err := os.ReadFile(filepath.Join(root, FileName)); err == nil {
			b = x
		}
	}
	t, err := parse(b)
	if err != nil {
		return err
	}
	mu.Lock()
	table = t
	mu.Unlock()
	return nil
}

func parse(b []byte) (map[string]Row, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))))
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	t := map[string]Row{}
	for i, x := range recs {
		if i == 0 || len(x) < 6 {
			continue
		}
		t[x[0]] = Row{x[0], strings.TrimSpace(x[1]), strings.TrimSpace(x[2]), x[3], x[4], strings.EqualFold(strings.TrimSpace(x[5]), "yes")}
	}
	return t, nil
}

func get(key string) (Row, bool) {
	mu.Lock()
	defer mu.Unlock()
	if table == nil {
		table, _ = parse(builtIn)
	}
	r, ok := table[key]
	return r, ok
}

// Cite gives "s. 36(4) (40A(3) of 1961)", with " (verify)" while unchecked. When the 2025
// section is not known yet: "40A(3) of 1961 (2025 section: verify)".
func Cite(key string) string {
	r, ok := get(key)
	if !ok {
		return key + " of 1961 (2025 section: verify)"
	}
	if r.S2025 == "" {
		return r.S1961 + " of 1961 (2025 section: verify)"
	}
	s := "s. " + r.S2025 + " (" + r.S1961 + " of 1961)"
	if !r.Verified {
		s += " (verify)"
	}
	return s
}
