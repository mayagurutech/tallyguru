# 8. Privacy

## Where your data is

- Books copied from Tally are kept in `%USERPROFILE%\TallyGuru`, one folder per company.
- Authorisation records and letter copies are encrypted with a key tied to your Windows account.
- Excel files are saved in `Documents\LedgerGuru`.
- The tool sends nothing to the author and has no cloud part. The only outside request is the
  optional update check, which reads GitHub's public release page.

## What the AI sees

The AI app sends what it reads to your AI service provider for processing. To keep that small:
- The server sends totals and short tables, not raw entries. Full detail goes to Excel on your PC.
- **Masking** (on by default) hides PAN, phone, bank account and Aadhaar numbers in answers.
- **Privacy mode** (off by default) shows party names to the AI as codes (P001...). Excel files
  keep real names, because they stay on your PC.
- Mapping sends only ledger names and groups, never vouchers.

Switch these in **Features**.

## Authorisation

Before the first sync of each company, you record your authorisation (or an owner's declaration)
for the financial year. It is renewed every year. See [2. First company](02-first-company.md).

## Deleting a client

Control panel > company card > **Delete local data**, then type the company name. This removes
that company's books copy, mapping, rules, drafts and authorisation record from this PC. Nothing in
Tally changes. Uninstalling asks whether to delete all client data.
