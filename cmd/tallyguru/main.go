// Command tallyguru is the Tally MCP by Guru server and admin CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
	"github.com/mayagurutech/tallyguru/internal/metrics"
	"github.com/mayagurutech/tallyguru/internal/statuspage"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

const version = "0.1.0-dev"

func usage() {
	fmt.Fprintf(os.Stderr, `Tally MCP by Guru %s
Not affiliated with or endorsed by Tally Solutions Pvt. Ltd.

Usage: tallyguru [flags] <command>

Commands:
  serve                run as an MCP server on stdio (what your AI app starts)
  setup                choose role profile and features
  ui                   open the control panel in your browser (setup, connection, companies,
                       mapping, features); this PC only
  doctor               check Tally, the data folder and the AI app connection
  ping                check TallyPrime answers on host:port
  companies            list companies open in TallyPrime
  authorise ["<company>"] [-owner | -letter <file> | -email <file>]
                       confirm you hold the business's authorisation (required before
                       sync, yearly). No company: pick from a list. Letter/email optional.
  sync "<company>"     copy books into the local database (month by month, incremental)
  mapping review|status|export|import "<company>" [file.csv]
                       ledger tags: "review" opens an easy page in your browser
  forget "<company>"   permanently delete all local data for a company (asks to confirm)

Flags:
`, version)
	flag.PrintDefaults()
}

func main() {
	host := flag.String("host", "localhost", "TallyPrime host")
	port := flag.Int("port", 9000, "TallyPrime XML port")
	source := flag.String("source", "live", "data source: live or backup")
	weekly := flag.Bool("weekly", false, "sync in weekly chunks (large companies)")
	full := flag.Bool("full", false, "force a full month-by-month sync (default: only what changed)")
	root := flag.String("data", store.DefaultRoot(), "local data folder")
	flag.Usage = usage
	if len(os.Args) > 1 && os.Args[1] != "authorise" && os.Args[1] != "authorize" {
		os.Args = append(os.Args[:1], flagsFirst(os.Args[1:])...)
	}
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	c := tally.NewClient(*host, *port)
	ctx := context.Background()
	var err error
	switch flag.Arg(0) {
	case "ping":
		if err = c.Ping(ctx); err == nil {
			fmt.Println("TallyPrime is reachable at", c.URL)
		}
	case "companies":
		var cs []tally.Company
		if cs, err = companies(ctx, c); err == nil {
			for _, co := range cs {
				fmt.Printf("%-40s GSTIN %-15s books from %s\n", co.Name, co.GSTIN, co.BooksFrom.Format("02-01-2006"))
			}
			if len(cs) == 0 {
				fmt.Println("No company is open in TallyPrime. Open \"TEST MCP\" and try again.")
			}
		}
	case "serve":
		err = runServe(*root)
	case "setup":
		err = runSetup(*root)
	case "ui":
		var cfg config.Config
		if cfg, err = config.Load(*root); err == nil {
			err = statuspage.Serve(*root, version, cfg, c, func(s string) { fmt.Println(s) })
		}
	case "doctor":
		var cfg config.Config
		if cfg, err = config.Load(*root); err == nil {
			bad := 0
			for _, k := range statuspage.Checks(ctx, *root, cfg, tally.NewClient(cfg.Host, cfg.Port)) {
				mark := "OK  "
				switch {
				case k.Warn:
					mark = "NOTE"
				case !k.OK:
					mark, bad = "FIX ", bad+1
				}
				fmt.Printf("%s %s: %s\n", mark, k.Name, k.Detail)
				if k.Fix != "" && !k.OK {
					fmt.Println("     " + k.Fix)
				}
			}
			if bad > 0 {
				os.Exit(1)
			}
		}
	case "forget":
		err = runForget(*root, strings.TrimSpace(flag.Arg(1)), store.Source(*source), os.Stdin)
	case "authorise", "authorize":
		err = runAuthorise(ctx, c, flag.Args()[1:], store.Source(*source), *root)
	case "mapping":
		err = runMapping(*root, flag.Args()[1:], store.Source(*source))
	case "sync":
		err = runSync(ctx, c, strings.TrimSpace(flag.Arg(1)), store.Source(*source), *weekly, *full, *root)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func companies(ctx context.Context, c *tally.Client) ([]tally.Company, error) {
	var cs []tally.Company
	err := c.Do(ctx, tally.CompanyListRequest().Build(), func(r io.Reader) error {
		return tally.ParseCompanies(r, func(co tally.Company) error { cs = append(cs, co); return nil })
	})
	return cs, err
}

func runSync(ctx context.Context, c *tally.Client, name string, src store.Source, weekly, full bool, root string) error {
	if name == "" {
		return fmt.Errorf(`give the company name, e.g. sync "TEST MCP"`)
	}
	_, idlePeak := metrics.RSS()
	t0 := time.Now()
	res, err := syncjob.Run(ctx, c, syncjob.Job{Root: root, Company: name, Source: src, Weekly: weekly,
		Full: full, Progress: func(s string) { fmt.Println(s) }})
	if res.Mode != "" {
		fmt.Printf("%s\n(total %s, RAM before sync %.1f MB)\n", res, time.Since(t0).Round(time.Millisecond), idlePeak)
	}
	return err
}

// flagsFirst lets options follow the command (`sync -full "X"`): Go's flag package stops at
// the first non-option, so options are moved to the front. Value options keep their value.
func flagsFirst(args []string) []string {
	takesValue := map[string]bool{"-host": true, "-port": true, "-source": true, "-data": true,
		"--host": true, "--port": true, "--source": true, "--data": true}
	var opts, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") && len(a) > 1 {
			opts = append(opts, a)
			if takesValue[a] && i+1 < len(args) {
				opts = append(opts, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return append(opts, rest...)
}
