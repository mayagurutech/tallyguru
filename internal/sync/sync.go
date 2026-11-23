// Package sync copies a company's books from TallyPrime into the local store, month by
// month (or week by week), fetching only what changed since the last sync.
package sync

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/mayagurutech/tallyguru/internal/metrics"
	"github.com/mayagurutech/tallyguru/internal/store"
	"github.com/mayagurutech/tallyguru/internal/tally"
)

// Options control a sync run.
type Options struct {
	From, To time.Time // period; zero From = company's books-from date, zero To = today
	Weekly   bool      // weekly chunks for large companies
	Progress func(string)
	// VouchersFrom: vouchers are kept from this date only (zero: the "sync_from" setting, else
	// all). Ledger openings are then Tally's balances on the day before, so every closing still
	// matches Tally. Used for companies with many years of history.
	VouchersFrom time.Time
	// Set by Smart: skip the voucher sweep, or fetch only vouchers changed after an AlterID.
	SkipVouchers bool
	ChangedSince int64
}

// MaxQuickChanges caps the one-request "changed vouchers" path; more changes than this
// use the monthly chunks so Tally never builds a big response.
const MaxQuickChanges = 2000

// Smart decides how much to fetch using the company's change counters (AltVchId, AltMstId):
//   - nothing changed and last check matched: no request at all ("unchanged")
//   - few voucher changes: masters + one request for changed vouchers ("quick")
//   - first sync, many changes, or changes that cannot be explained (e.g. deletions): full
//     monthly sweep ("full"). The trial-balance check afterwards catches anything missed.
func (e *Engine) Smart(ctx context.Context, o Options, co tally.Company) (Stats, string, error) {
	gi := func(k string) int64 { v, _ := e.DB.Meta(k); n, _ := strconv.ParseInt(v, 10, 64); return n }
	prevV, prevM := gi("alt_vch_id"), gi("alt_mst_id")
	tb, _ := e.DB.Meta("tb_status")
	save := func() {
		e.DB.SetMeta("alt_vch_id", strconv.FormatInt(co.AltVchID, 10))
		e.DB.SetMeta("alt_mst_id", strconv.FormatInt(co.AltMstID, 10))
	}
	if prevV > 0 && prevV == co.AltVchID && prevM == co.AltMstID && tb == "ok" {
		e.DB.SetMeta("last_sync", time.Now().Format(time.RFC3339))
		return Stats{}, "unchanged", nil
	}
	mode := "full"
	if prevV > 0 && tb == "ok" {
		switch delta := co.AltVchID - prevV; {
		case delta == 0:
			o.SkipVouchers, mode = true, "masters only"
		case delta > 0 && delta <= MaxQuickChanges:
			o.ChangedSince, mode = prevV, "quick"
		}
	}
	st, err := e.Run(ctx, o)
	if err != nil {
		return st, mode, err
	}
	// Quick path fetched fewer vouchers than the counter moved: something was deleted or
	// edited more than once. Sweep months to mirror deletions exactly.
	// (A voucher saved twice also moves the counter twice; the sweep is then harmless:
	// unchanged months fetch nothing.)
	if mode == "quick" && int64(st.Fetched) < co.AltVchID-prevV {
		o.ChangedSince = 0
		var st2 Stats
		st2, err = e.Run(ctx, o)
		st2.Requests += st.Requests
		st, mode = st2, "quick + full sweep (possible deletions)"
	}
	if err == nil {
		save()
	}
	return st, mode, err
}

// changed fetches vouchers altered/created after sinceAlterID in one request and upserts them.
func (e *Engine) changed(ctx context.Context, company string, since int64, from, to time.Time, dated bool) (int, error) {
	tx, err := e.DB.BeginVouchers()
	if err != nil {
		return 0, err
	}
	req := tally.ChangedVouchersRequest(company, since, from, to)
	if dated { // changes to vouchers before the synced period are in the openings, not here
		req.Filters = append(req.Filters, tally.DateFilter(from, to))
	}
	err = e.C.Do(ctx, req.Build(), func(r io.Reader) error {
		return tally.ParseVouchers(r, func(v tally.Voucher) error {
			if tx.N >= MaxQuickChanges*2 {
				return fmt.Errorf("more changed vouchers than expected; use a full sync")
			}
			return tx.Put(v)
		})
	})
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	return tx.N, tx.Commit()
}

// Stats summarise a run.
type Stats struct {
	Chunks, Requests, Fetched, Deleted, Ledgers int
	Duration                                    time.Duration
	PeakRSSMB                                   float64
}

func (s Stats) String() string {
	return fmt.Sprintf("%d chunks, %d Tally requests, %d vouchers fetched, %d deleted, %d ledgers, %s, peak RAM %.1f MB",
		s.Chunks, s.Requests, s.Fetched, s.Deleted, s.Ledgers, s.Duration.Round(time.Millisecond), s.PeakRSSMB)
}

// Engine runs syncs for one company.
type Engine struct {
	C  *tally.Client
	DB *store.DB
}

// Run performs one sync.
func (e *Engine) Run(ctx context.Context, o Options) (Stats, error) {
	start := time.Now()
	var st Stats
	say := func(f string, a ...any) {
		if o.Progress != nil {
			o.Progress(fmt.Sprintf(f, a...))
		}
	}
	company := e.DB.Label.Company

	// 0. The whole synced period, stated in every request (Tally otherwise uses the
	// company's current period and hides other years). Default: books-from date to the
	// end of the financial year containing today.
	if o.From.IsZero() {
		bf, _ := e.DB.Meta("books_from")
		t, err := time.Parse(time.DateOnly, bf)
		if err != nil {
			return st, fmt.Errorf("books-from date unknown; pass a From date")
		}
		o.From = t
	}
	if o.To.IsZero() {
		o.To = FYEnd(time.Now())
	}
	vf := VouchersFrom(e.DB, o)

	// 1. Ledgers: always the full list (few fields, small), so deletions are caught.
	var ls []tally.Ledger
	st.Requests++
	if err := e.C.Do(ctx, tally.LedgerRequest(company, o.From, o.To).Build(), func(r io.Reader) error {
		return tally.ParseLedgers(r, func(l tally.Ledger) error { ls = append(ls, l); return nil })
	}); err != nil {
		return st, fmt.Errorf("ledgers: %w", err)
	}
	if err := e.DB.ReplaceLedgers(ls); err != nil {
		return st, err
	}
	st.Ledgers = len(ls)
	say("ledgers: %d", len(ls))

	// 1b. Groups, voucher types and stock items: small masters, always full.
	var gs []tally.Group
	var vts []tally.VoucherType
	var sis []tally.StockItem
	var bills []tally.Bill
	for _, m := range []struct {
		req   tally.CollectionRequest
		parse func(io.Reader) error
	}{
		{tally.GroupRequest(company), func(r io.Reader) error {
			return tally.ParseGroups(r, func(g tally.Group) error { gs = append(gs, g); return nil })
		}},
		{tally.VoucherTypeRequest(company), func(r io.Reader) error {
			return tally.ParseVoucherTypes(r, func(v tally.VoucherType) error { vts = append(vts, v); return nil })
		}},
		{tally.StockItemRequest(company, o.From, o.To), func(r io.Reader) error {
			return tally.ParseStockItems(r, func(s tally.StockItem) error { sis = append(sis, s); return nil })
		}},
		{tally.BillRequest(company, o.From, o.To), func(r io.Reader) error {
			return tally.ParseBills(r, func(b tally.Bill) error { bills = append(bills, b); return nil })
		}},
	} {
		st.Requests++
		if err := e.C.Do(ctx, m.req.Build(), m.parse); err != nil {
			return st, fmt.Errorf("%s: %w", m.req.ID, err)
		}
	}
	if err := e.DB.ReplaceGroups(gs); err != nil {
		return st, err
	}
	if err := e.DB.ReplaceVoucherTypes(vts); err != nil {
		return st, err
	}
	if err := e.DB.ReplaceStockItems(sis); err != nil {
		return st, err
	}
	if err := e.DB.ReplaceBills(bills); err != nil {
		return st, err
	}
	say("groups: %d, voucher types: %d, stock items: %d, open bills: %d", len(gs), len(vts), len(sis), len(bills))
	if vf.After(o.From) {
		st.Requests++
		if err := e.openingsAt(ctx, company, o.From, vf); err != nil {
			return st, fmt.Errorf("balances on %s: %w", vf.AddDate(0, 0, -1).Format(time.DateOnly), err)
		}
		say("vouchers from %s; ledger openings are Tally's balances on the day before", vf.Format(time.DateOnly))
	}

	// 2. Vouchers, one chunk at a time (period was fixed above).
	chunks := tally.Chunks(vf, o.To, o.Weekly)
	switch {
	case o.SkipVouchers:
		chunks = nil
		say("vouchers unchanged in Tally; skipped")
	case o.ChangedSince > 0:
		chunks = nil
		n, err := e.changed(ctx, company, o.ChangedSince, vf, o.To, vf.After(o.From))
		st.Requests++
		if err != nil {
			return st, fmt.Errorf("changed vouchers: %w", err)
		}
		st.Fetched += n
		say("changed vouchers fetched: %d", n)
	}
	for _, p := range chunks {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		f, d, n, err := e.chunk(ctx, company, p)
		st.Requests += n
		if err != nil {
			return st, fmt.Errorf("%s to %s: %w", p.From.Format(time.DateOnly), p.To.Format(time.DateOnly), err)
		}
		st.Chunks++
		st.Fetched += f
		st.Deleted += d
		_, peak := metrics.RSS()
		say("%s: %d fetched, %d deleted (RAM peak %.1f MB)", p.From.Format("Jan 2006")+weekSuffix(o.Weekly, p), f, d, peak)
	}
	t0 := time.Now()
	if n, err := e.DB.ResolveNames(); err != nil {
		return st, fmt.Errorf("ledger names: %w", err)
	} else if n > 0 {
		say("%d ledger names in vouchers written differently from the ledger (case, spaces or dots) matched to the ledger", n)
	}
	if err := e.DB.RebuildSummaries(); err != nil {
		return st, fmt.Errorf("summaries: %w", err)
	}
	say("summaries rebuilt in %s", time.Since(t0).Round(time.Millisecond))
	st.Duration = time.Since(start)
	_, st.PeakRSSMB = metrics.RSS()
	e.DB.SetMeta("last_sync", time.Now().Format(time.RFC3339))
	return st, nil
}

func weekSuffix(w bool, p tally.Period) string {
	if !w {
		return ""
	}
	return " wk " + p.From.Format("02")
}

// chunk syncs one period: light ID list first, then full detail only for new/changed.
func (e *Engine) chunk(ctx context.Context, company string, p tally.Period) (fetched, deleted, requests int, err error) {
	// Tally's view of the period.
	remote := map[string]int64{}
	requests++
	if err = e.C.Do(ctx, tally.VoucherIDsRequest(company, p.From, p.To).Build(), func(r io.Reader) error {
		return tally.ParseVouchers(r, func(v tally.Voucher) error { remote[v.GUID] = v.AlterID; return nil })
	}); err != nil {
		return
	}
	// Our view of the same period.
	local := map[string]int64{}
	rows, err := e.DB.Query(`SELECT guid, alter_id FROM vouchers WHERE date BETWEEN ? AND ?`,
		p.From.Format(time.DateOnly), p.To.Format(time.DateOnly))
	if err != nil {
		return
	}
	for rows.Next() {
		var g string
		var a int64
		rows.Scan(&g, &a)
		local[g] = a
	}
	rows.Close()

	var minChanged int64 = -1
	for g, a := range remote {
		if la, ok := local[g]; !ok || la != a {
			if minChanged < 0 || a < minChanged {
				minChanged = a
			}
		}
	}

	tx, err := e.DB.BeginVouchers()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()
	// Deleted in Tally (or moved out of this period: re-fetched by its new period).
	for g := range local {
		if _, ok := remote[g]; !ok {
			if err = tx.Delete(g); err != nil {
				return
			}
			deleted++
		}
	}
	if minChanged >= 0 {
		requests++
		err = e.C.Do(ctx, tally.VoucherRequest(company, p.From, p.To, minChanged-1).Build(), func(r io.Reader) error {
			return tally.ParseVouchers(r, func(v tally.Voucher) error {
				if la, ok := local[v.GUID]; ok && la == v.AlterID {
					return nil // unchanged
				}
				return tx.Put(v)
			})
		})
		if err != nil {
			return
		}
	}
	fetched = tx.N
	err = tx.Commit()
	return
}

// VouchersFrom is the first date whose vouchers are kept: o.VouchersFrom, else the company's
// "sync_from" setting, never before o.From (the books-from date).
func VouchersFrom(db *store.DB, o Options) time.Time {
	vf := o.VouchersFrom
	if vf.IsZero() {
		v, _ := db.Meta("sync_from")
		vf, _ = time.Parse(time.DateOnly, v)
	}
	if vf.Before(o.From) {
		return o.From
	}
	return vf
}

// plLedger is Tally's reserved profit & loss ledger (tbcheck.PLLedger).
const plLedger = "Profit & Loss A/c"

// openingsAt sets every ledger's opening to its Tally balance on the day before vf (one
// request, the same size as the ledger list) and drops vouchers dated before vf.
func (e *Engine) openingsAt(ctx context.Context, company string, booksFrom, vf time.Time) error {
	bal := map[string]int64{}
	err := e.C.Do(ctx, tally.LedgerOpeningsRequest(company, booksFrom, vf.AddDate(0, 0, -1)).Build(), func(r io.Reader) error {
		return tally.ParseLedgers(r, func(l tally.Ledger) error { bal[l.Name] = int64(l.Closing); return nil })
	})
	if err != nil {
		return err
	}
	tx, err := e.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Stock-in-Hand ledgers keep their master opening: Tally reports their balance as the stock
	// valuation, not as entries. Profit & Loss A/c: Tally's balance includes the profit the
	// income and expense ledgers already carry, so its opening is what makes the books total
	// zero (exact, since every other opening is Tally's own figure).
	for n, b := range bal {
		if _, err := tx.Exec(`UPDATE ledgers SET opening = ? WHERE name = ? AND name <> ? AND parent <> 'Stock-in-Hand'
			AND parent NOT IN (SELECT name FROM group_anc WHERE ancestor = 'Stock-in-Hand')`, b, n, plLedger); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE ledgers SET opening = -(SELECT COALESCE(SUM(opening), 0) FROM ledgers WHERE name <> ?)
		WHERE name = ?`, plLedger, plLedger); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM vouchers WHERE date < ?`, vf.Format(time.DateOnly)); err != nil {
		return err
	}
	return tx.Commit()
}

// SetBooksFrom records the company's books-from date (from the company list).
func SetBooksFrom(db *store.DB, t time.Time) error {
	return db.SetMeta("books_from", t.Format(time.DateOnly))
}

// FYEnd returns 31 March of the Indian financial year containing t.
func FYEnd(t time.Time) time.Time {
	y := t.Year()
	if t.Month() >= time.April {
		y++
	}
	return time.Date(y, time.March, 31, 0, 0, 0, 0, time.UTC)
}

// Refetch reads the given periods again from Tally in full (local vouchers of those dates are
// dropped first), then matches names and rebuilds summaries. Used by the trial-balance self-heal.
func (e *Engine) Refetch(ctx context.Context, ps []tally.Period) (fetched int, err error) {
	company := e.DB.Label.Company
	for _, p := range ps {
		if _, err := e.DB.Exec(`DELETE FROM vouchers WHERE date BETWEEN ? AND ?`,
			p.From.Format(time.DateOnly), p.To.Format(time.DateOnly)); err != nil {
			return fetched, err
		}
		f, _, _, err := e.chunk(ctx, company, p)
		if err != nil {
			return fetched, fmt.Errorf("%s: %w", p.From.Format("Jan 2006"), err)
		}
		fetched += f
	}
	if _, err := e.DB.ResolveNames(); err != nil {
		return fetched, err
	}
	return fetched, e.DB.RebuildSummaries()
}
