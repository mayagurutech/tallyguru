# 4. Reports and Excel

Ask the AI in plain words. It uses the report tool; the server does every sum and sends back a short
table. Full detail goes to Excel on your PC, not into the chat.

| Ask for | Example |
|---|---|
| Owner's summary | "Give me the MIS for this year" |
| Profit and loss | "P&L for April to September", "P&L in Schedule III layout" |
| Balance sheet | "Balance sheet as on 30 September", "Schedule III balance sheet" |
| One group's ledgers | "Show the ledgers under Indirect Expenses" |
| Ratios and cash flow | "Ratios for 2026-27", "Cash flow for the half year" |
| GST and TDS (books only) | "GST summary month-wise", "TDS by section" |
| Ageing | "Receivables ageing", "Payables ageing in Excel" |
| Day Book | "Day Book for August in Excel", "All payment vouchers in July" |
| Trial balance, ledgers, outstanding | "Trial balance", "Ledger of Ram Traders with the last 10 entries", "Top debtors" |

Periods: a financial year like 2026-27, or months from and to (2026-04 to 2026-09). Without a
period, the current year to date is used.

## Excel files

Add "in Excel" to the question. The file is saved in `Documents\LedgerGuru\<company>` and
the AI gets only its path. The control panel lists recent files; **Open Excel folder** opens the
folder. Excel files keep real names even in privacy mode, because they stay on your PC.

## Numbers should match Tally

Reports are built from the synced books, and the trial balance is checked against Tally after
every sync. If the check fails, answers carry a warning. Run **Full re-check** in the control
panel.

## Portal reconciliation

GSTR-1, 3B and 2B, and 26Q/26AS, are not read by this tool. Download them from the portal and give
them to your AI app (or a skill) together with the GST and TDS summaries from here.
