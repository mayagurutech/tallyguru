# 6. Entering vouchers safely

Writing to Tally is off unless your role allows it (Accountant) and **Read-only** is off in
**Features**. For a real client, the first posting asks you to allow it (see below).

## How every entry works

1. **Draft.** You tell the AI the entry: "Payment of 25,000 rent to Sharma from HDFC Bank on
   1 July". The server checks it and shows a **preview** with a draft number. Nothing is in Tally
   yet.
2. **Check the preview.** Look at the ledgers, amounts, date and the warnings.
3. **Post.** Say "post it", or post from the control panel (**Drafts and writes**). Only drafts
   that were previewed can be posted.

After posting, the books are synced and the trial balance is checked again. Every entry gets
"Posted via Tally MCP by Guru" in its narration and a line in the write log.

## Checks on every draft

- Ledgers must exist in Tally. Close names are suggested, never picked for you.
- Debit must equal credit, and the date must be inside the books.
- Duplicates are refused: same party, same bill number, same financial year.
- GST warnings: IGST vs CGST+SGST by party state, an unusual rate, CGST different from SGST.
- A missing TDS line, and ITC on a blocked-credit ledger, are flagged.

Supported: payment, receipt, journal, contra, sales and purchase (accounting or item invoice),
credit and debit notes, stock journal, and masters (ledger, group, stock item, unit, cost centre).

## Alter, cancel, delete

Switch on **Alter / cancel / delete vouchers** in Features. The preview shows the voucher **now**
and **after**, and a one-time word such as `CANCEL-5524`. **You** type that word to post the change.
You choose cancel or delete each time. The original voucher is kept in the write log.

Sales, purchase and note vouchers can be cancelled or deleted here; alter those in Tally.

## Writing to a real client

TEST MCP (a dummy company) is always writable. For any other company, drafts, previews, bank
statement matching and rules work at once; only posting to Tally needs your permission. The
first time you confirm a posting for that company, a Windows box asks "Allow posting to ...?":
click **Yes**. Only you can answer it; the AI cannot. It is remembered for that company.
(You can also switch it on or off in the control panel: the company's **Drafts and writes**.)

- **Take a Tally backup** (Alt+Y) before posting.
- Switch it off again in **Drafts and writes** when you are done, if you want.

## Carrying balances to a new year's company

When last year's closing balances are entered as this year's opening balances:

- **Closing stock** is Tally's closing stock value (shown as "Closing stock, Tally's value" in the
  trial balance and ledger answers), not the stock ledger's entries.
- If the new company keeps inventory, opening stock goes on the **stock items** (opening
  quantity and value), and their total must equal last year's closing stock. The tool refuses
  an opening on the Stock-in-Hand ledger there.
- **Profit & Loss A/c** opening is last year's accumulated profit, never a balancing figure. The
  tool warns on every P&L opening draft. If the openings don't tally, check closing stock first.
