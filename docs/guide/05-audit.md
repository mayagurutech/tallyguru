# 5. Audit checks

Switch on **Audit checks** in the control panel (the Auditor role has it on). Then ask the AI, for
example: "Run the audit checks for 2026-27" or "Show the cash payment cases".

The checks run on **every entry** of the period, never a sample. They report facts: what was
found, where and how much. Whether something is a disallowance or a default is **your judgment**;
the tool does not conclude and does not sign anything off.

## The checks

| Check | Rule |
|---|---|
| Cash payments, s. 36(4) (40A(3) of 1961) | Over 10,000 to one party in a day; 35,000 for transporters marked in Tally |
| Cash receipts, s. 186 (269ST) | 2 lakh or more from one party in a day |
| Loans taken / repaid in cash, s. 185 (269SS/269T) | 20,000 or more |
| MSME over 45 days, s. 37 (43B(h)) | Creditors you tagged MSME, unpaid beyond 45 days at the period end |
| TDS not deducted, s. 35 (40(a)(ia)) | Expenses over the TDS threshold with no TDS against the party |
| Related parties | Entries with ledgers you tagged related |
| Negative cash, suspense | Days cash went negative; suspense balances and entries |
| Duplicates | Same type, party and amount within 7 days (10,000 or more, or the same bill reference) |
| Round sums | Exact multiples of 10,000, 50,000 or more |
| Sunday entries | Entries dated on Sundays |
| Old receivables | Over 180 days, and advances to suppliers |
| GSTIN | Format, state code, check digit; GSTIN state vs ledger state |
| IGST vs state | IGST to a same-state party, or CGST+SGST to another state |
| Foreign parties | Country not India (kept out of 194C and GST checks; review TDS on non-residents) |

Sections print as Income-tax Act 2025 first, with the 1961 section in brackets. Rows marked
"(verify)" are waiting for the section table to be confirmed.

## The Excel workbook

Ask "audit checks in Excel". The workbook has three sheets:
- **Summary**: one line per check, with count and amount.
- **Issues**: every case, with a remark column for you.
- **Working**: every entry behind each case, so you can tick them.

## What it cannot see

- An edit log, when Tally's Edit Log is off.
- MSME status and related parties, unless you tag them ([3. Mapping](03-mapping.md)).
- PAN, when it isn't filled in Tally.
- Payee names hidden in narrations, for example "cash to staff".
