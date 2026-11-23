// Package tally talks to TallyPrime over its documented XML-over-HTTP interface.
// All TDL here is our own inline collection definitions; nothing is installed in Tally.
package tally

import (
	"fmt"
	"strings"
	"time"
)

// xmlEscape escapes text for element content and attribute values.
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// TallyDate formats a date for SVFROMDATE/SVTODATE. Observed on TallyPrime: "20260401" is
// misread (it narrowed the period to a single day), while "1-Apr-2026" sets the period. Every
// request must state its period: without it Tally uses the company's current period (Alt+F2)
// and silently hides other years (pilot 1 showed 5,265 of 12,517 vouchers).
func TallyDate(t time.Time) string { return t.Format("2-Jan-2006") }

// CollectionRequest describes one inline-TDL collection export.
type CollectionRequest struct {
	ID       string    // collection name used in the request
	Type     string    // Tally object type: Company, Group, Ledger, Voucher, StockItem ...
	Fetch    []string  // only the fields we need
	Company  string    // SVCURRENTCOMPANY; empty for company list
	From, To time.Time // optional period (vouchers); zero = not sent
	Filters  []string  // TDL formulae, all must be true, e.g. "$AlterID > 120"
}

// Build returns the XML envelope for the request.
func (c CollectionRequest) Build() string {
	var b strings.Builder
	b.WriteString("<ENVELOPE><HEADER><VERSION>1</VERSION><TALLYREQUEST>Export</TALLYREQUEST>")
	b.WriteString("<TYPE>Collection</TYPE><ID>" + xmlEscape(c.ID) + "</ID></HEADER>")
	b.WriteString("<BODY><DESC><STATICVARIABLES><SVEXPORTFORMAT>$$SysName:XML</SVEXPORTFORMAT>")
	if c.Company != "" {
		b.WriteString("<SVCURRENTCOMPANY>" + xmlEscape(c.Company) + "</SVCURRENTCOMPANY>")
	}
	if !c.From.IsZero() {
		b.WriteString(`<SVFROMDATE TYPE="Date">` + TallyDate(c.From) + "</SVFROMDATE>")
		b.WriteString(`<SVTODATE TYPE="Date">` + TallyDate(c.To) + "</SVTODATE>")
	}
	b.WriteString("</STATICVARIABLES><TDL><TDLMESSAGE>")
	b.WriteString(`<COLLECTION NAME="` + xmlEscape(c.ID) + `" ISMODIFY="No">`)
	b.WriteString("<TYPE>" + xmlEscape(c.Type) + "</TYPE>")
	if len(c.Fetch) > 0 {
		b.WriteString("<FETCH>" + xmlEscape(strings.Join(c.Fetch, ",")) + "</FETCH>")
	}
	for i := range c.Filters {
		b.WriteString(fmt.Sprintf("<FILTER>GuruF%d</FILTER>", i))
	}
	b.WriteString("</COLLECTION>")
	for i, f := range c.Filters {
		b.WriteString(fmt.Sprintf(`<SYSTEM TYPE="Formulae" NAME="GuruF%d">%s</SYSTEM>`, i, xmlEscape(f)))
	}
	b.WriteString("</TDLMESSAGE></TDL></DESC></BODY></ENVELOPE>")
	return b.String()
}

// CompanyListRequest lists companies open in Tally.
func CompanyListRequest() CollectionRequest {
	return CollectionRequest{ID: "GuruCompanies", Type: "Company",
		Fetch: []string{"Name", "GUID", "StartingFrom", "BooksFrom", "GSTRegistrationNumber", "StateName", "AlterID", "AltVchId", "AltMstId"}}
}

// LedgerRequest lists all ledgers. The period [from,to] must cover the whole synced range:
// ClosingBalance is computed by Tally for that period (without it, for the current period only).
func LedgerRequest(company string, from, to time.Time) CollectionRequest {
	return CollectionRequest{ID: "GuruLedgers", Type: "Ledger", Company: company, From: from, To: to,
		Fetch: []string{"Name", "GUID", "Parent", "OpeningBalance", "ClosingBalance", "AlterID", "MasterID",
			// Party details for GST/TDS checks (Phase 4b). Current TallyPrime keeps GSTIN, state and
			// the transporter flag in the dated registration list; PartyGSTIN and LedStateName are
			// the older fields, used as a fallback. No PAN: rarely kept in Tally (pilot: 2 of 451).
			"LedGSTRegDetails", "PartyGSTIN", "LedStateName", "CountryOfResidence", "CurrencyName"}}
}

// VoucherIDsRequest lists only GUID + AlterID for a period: a light request used to
// detect new, altered and deleted vouchers before fetching full detail.
func VoucherIDsRequest(company string, from, to time.Time) CollectionRequest {
	return CollectionRequest{ID: "GuruVchIDs", Type: "Voucher", Company: company, From: from, To: to,
		Fetch: []string{"GUID", "AlterID", "Date"}, Filters: []string{dateFilter(from, to)}}
}

// DateFilter is dateFilter, for callers outside this package.
func DateFilter(from, to time.Time) string { return dateFilter(from, to) }

// dateFilter limits a Voucher collection to [from,to] with date literals. Kept in addition to
// the SV period (belt and braces): early tests showed requests returning vouchers outside the
// requested month when the period was sent in the wrong format.
func dateFilter(from, to time.Time) string {
	return fmt.Sprintf(`$Date >= $$Date:"%s" AND $Date <= $$Date:"%s"`, from.Format("02-Jan-2006"), to.Format("02-Jan-2006"))
}

// VoucherRequest lists vouchers in one chunk (never a full year — caller enforces).
func VoucherRequest(company string, from, to time.Time, sinceAlterID int64) CollectionRequest {
	r := CollectionRequest{ID: "GuruVouchers", Type: "Voucher", Company: company, From: from, To: to,
		Fetch: []string{"Date", "GUID", "AlterID", "MasterID", "VoucherTypeName", "VoucherNumber",
			"PartyLedgerName", "Reference", "IsCancelled", "IsOptional", "IsPostDated", "Narration",
			"AllLedgerEntries.LedgerName", "AllLedgerEntries.Amount"},
		Filters: []string{dateFilter(from, to)}}
	if sinceAlterID > 0 {
		r.Filters = append(r.Filters, fmt.Sprintf("$AlterID > %d", sinceAlterID))
	}
	return r
}

// GroupRequest lists account groups (small; always full).
func GroupRequest(company string) CollectionRequest {
	return CollectionRequest{ID: "GuruGroups", Type: "Group", Company: company,
		Fetch: []string{"Name", "Parent", "IsRevenue", "AffectsGrossProfit", "ReservedName"}}
}

// VoucherTypeRequest lists voucher types with their base type.
func VoucherTypeRequest(company string) CollectionRequest {
	return CollectionRequest{ID: "GuruVchTypes", Type: "VoucherType", Company: company,
		Fetch: []string{"Name", "Parent"}}
}

// StockItemRequest lists stock items with closing quantity and value.
func StockItemRequest(company string, from, to time.Time) CollectionRequest {
	return CollectionRequest{ID: "GuruStock", Type: "StockItem", Company: company, From: from, To: to,
		Fetch: []string{"Name", "Parent", "BaseUnits", "ClosingBalance", "ClosingValue", "OpeningValue"}}
}

// ChangedVouchersRequest lists vouchers altered or created after the given AlterID, across
// all dates. Used only when the company's change counter shows a small number of changes,
// so the response stays small (the caller caps it and otherwise falls back to monthly chunks).
func ChangedVouchersRequest(company string, sinceAlterID int64, from, to time.Time) CollectionRequest {
	r := VoucherRequest(company, from, to, sinceAlterID) // period = whole synced range
	r.ID, r.Filters = "GuruChanged", []string{fmt.Sprintf("$AlterID > %d", sinceAlterID)}
	return r
}

// BillRequest lists Tally's outstanding bills (bill-wise parties) for the period.
// Small: one row per open bill, computed by Tally.
func BillRequest(company string, from, to time.Time) CollectionRequest {
	return CollectionRequest{ID: "GuruBills", Type: "Bill", Company: company, From: from, To: to,
		Fetch: []string{"Name", "Parent", "BillDate", "ClosingBalance", "IsAdvance"}}
}

// LedgerOpeningsRequest gives every ledger's balance at the end of [from,to] (ClosingBalance):
// the openings when only vouchers from to+1 are synced.
func LedgerOpeningsRequest(company string, from, to time.Time) CollectionRequest {
	return CollectionRequest{ID: "GuruLedgerOpen", Type: "Ledger", Company: company, From: from, To: to,
		Fetch: []string{"Name", "GUID", "Parent", "OpeningBalance", "ClosingBalance"}}
}

// LedgerBalancesRequest gives the named ledgers' closing balances for [from,to]: used by the
// trial-balance self-heal, one month end at a time, for the few ledgers that differ.
func LedgerBalancesRequest(company string, from, to time.Time, names []string) CollectionRequest {
	var or []string
	for _, n := range names {
		or = append(or, `$Name = "`+strings.ReplaceAll(n, `"`, `""`)+`"`)
	}
	return CollectionRequest{ID: "GuruLedgerBal", Type: "Ledger", Company: company, From: from, To: to,
		Fetch: []string{"Name", "GUID", "Parent", "OpeningBalance", "ClosingBalance"}, Filters: []string{strings.Join(or, " OR ")}}
}
