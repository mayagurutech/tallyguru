package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/syncjob"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// runDoctorTB syncs every company open in Tally that has a valid authorisation (read-only on
// Tally, as every sync is) and prints one line per company with counts only: no company names,
// ledger names or amounts, so the output can be shared when asking for help.
func runDoctorTB(ctx context.Context, c *tally.Client, root string, src store.Source) error {
	cs, err := syncjob.Companies(ctx, c)
	if err != nil {
		return err
	}
	if len(cs) == 0 {
		return fmt.Errorf("no company is open in TallyPrime")
	}
	fmt.Printf("Trial-balance check of %d open companies (source %s). Counts only; no names or amounts.\n", len(cs), src)
	bad := 0
	for i, co := range cs {
		lead := fmt.Sprintf("Company %d:", i+1)
		res, err := syncjob.Run(ctx, c, syncjob.Job{Root: root, Company: co.Name, Source: src})
		tb := res.TB
		switch {
		case err != nil && strings.Contains(err.Error(), "authoris"):
			fmt.Println(lead, "skipped, no authorisation recorded for this year")
			continue
		case err != nil && tb.Ledgers == 0:
			bad++
			fmt.Println(lead, "sync failed:", clip(err.Error()))
			continue
		}
		status := "TIES"
		if !tb.OK() {
			status, bad = "DIFFERS", bad+1
		}
		fmt.Printf("%s %s. %d ledgers, %d vouchers fetched, mode %s; %d ledgers differ, %d unknown ledgers, %d stock ledgers not compared, %d months re-read by the self-check, %d months still differ\n",
			lead, status, tb.Ledgers, res.Stats.Fetched, res.Mode, len(tb.Mismatches), len(tb.Unknown), len(tb.Stock),
			len(res.Heal.Refetched), len(res.Heal.Left))
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d companies need attention; open the control panel for details", bad, len(cs))
	}
	fmt.Println("All checked companies tie with Tally.")
	return nil
}

func clip(s string) string {
	if i := strings.IndexByte(s, '"'); i >= 0 {
		return s[:i] + "(name hidden)"
	}
	return s
}
