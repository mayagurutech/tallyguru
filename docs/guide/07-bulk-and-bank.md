# 7. Bulk import and bank statements

Switch on **Bulk import** and/or **Bank statements and reconciliation** in Features. Your AI app
reads the file (Excel, PDF, photo); this tool never opens documents. It only receives the rows,
checks them and keeps drafts.

## Bulk import

Give the AI the file and say what it is: "Import these 300 sales invoices for October as group
'Sales Oct 2026'". The AI sends 50 entries at a time. For each part you get:
- how many were drafted, with totals by voucher type;
- the rows refused, by row number, and why (for example "row 7: ledger not found, did you mean
  ...?");
- warnings.

Fix the refused rows and send them again. Post the whole group from the control panel (**Post
group**) or by asking the AI, after you have checked the totals. Measured: 1,000 vouchers were
posted in about a minute.

## Bank statements

**Scanned statements and photos use more AI tokens than PDF or Excel. The AI will ask before
reading one.**

1. "Reconcile the HDFC Bank statement for July." The AI sends the statement **page by page**.
2. Each page is checked: every running balance, opening plus credits minus debits equals the
   closing, and each page opens where the last one closed. A page that fails is refused with the
   wrong line named, and the AI reads that page again.
3. Matching: same amount and side, within 3 days; a cheque or reference number decides between
   look-alikes. You get:
   - **Matched** entries.
   - **Probable** matches, for you to confirm.
   - **Not in books** lines.
   - **Not in the statement** book entries.
4. **Bank reconciliation statement** as on the last statement date, ending in "Ties" or an
   unexplained difference. Ask "in Excel" for the full Reco, Statement and Books sheets.
5. For statement lines not in the books, the tool **drafts** payments or receipts when it can
   guess the ledger. Bank charges and interest are always left for you to decide (unless you saved
   a rule for them). Nothing posts until you confirm.

## Rules memory

When you correct a suggested ledger, the AI asks whether to remember it for this client. For
example, "BANK CHG" goes to Bank Charges. Your rules are used first, then past entries, then
ledger names, and each suggestion says which one it used. See and delete rules in the control
panel (**Drafts and writes > Remembered rules**). Rules belong to one client only.
