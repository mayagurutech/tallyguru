package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/i18n"
	"github.com/mayagurutech/tallyguru/internal/store"
)

// runForget deletes everything stored for one company (one source): books database,
// authorisation records and the encrypted letter copy. Works without Tally.
func runForget(root, name string, src store.Source, in io.Reader) error {
	if name == "" {
		return fmt.Errorf(`usage: tallyguru [-source live|backup] forget "<company>"`)
	}
	ls, err := store.List(root)
	if err != nil {
		return err
	}
	var dir string
	for _, l := range ls {
		if strings.EqualFold(l.Company, name) && l.Source == src {
			dir = l.Dir(root)
		}
	}
	if dir == "" {
		// Authorised but never synced: folder exists without books.db.
		cand := store.Label{Company: name, Source: src}.Dir(root)
		if fi, err := os.Stat(cand); err == nil && fi.IsDir() {
			dir = cand
		}
	}
	if dir == "" {
		return fmt.Errorf("no stored data for %q (%s) under %s", name, src, root)
	}
	if filepath.Dir(dir) != filepath.Clean(root) {
		return fmt.Errorf("refusing: %s is not a company folder inside %s", dir, root)
	}

	var files []string
	var total int64
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			files = append(files, fmt.Sprintf("  %-40s %8d bytes", filepath.Base(p), fi.Size()))
			total += fi.Size()
		}
		return nil
	})
	L := lang(root)
	fmt.Println(i18n.T(L, "forget_header", name, src))
	fmt.Printf("%s\n%s\n  total %d bytes in %d files\n", dir, strings.Join(files, "\n"), total, len(files))
	fmt.Println(i18n.T(L, "forget_tally"))
	fmt.Println(i18n.T(L, "forget_close"))
	fmt.Print(i18n.T(L, "forget_confirm"))
	line, _ := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) != name {
		return fmt.Errorf("%s", i18n.T(L, "forget_mismatch"))
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("could not delete everything (is the AI app still running?): %w", err)
	}
	fmt.Println(i18n.T(L, "forget_done", dir))
	return nil
}
