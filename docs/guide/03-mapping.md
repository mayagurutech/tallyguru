# 3. Ledger mapping

Mapping tells the tool what each ledger is: rent (TDS 194I), professional fees (194J), contractor
(194C), GST input or output, blocked credit 17(5), related party, MSME supplier, loan, suspense and
so on. Audit checks, GST checks and entry warnings use these tags.

The tool suggests a tag for every ledger from its name, its group and how it is used in the books.
You confirm once per client; your answers are kept and reused, even after ledgers are renamed.

## Review

In the control panel, on the company card, click **Review mapping** (or **Answer N mapping
questions** when some need you). A page opens with:
- Ledgers the tool decided from the books, which you can change.
- Questions only you can answer, for example whether "CGST @ 9%" is input or output, or whether
  194Q applies (it depends on turnover).

Blocked credit 17(5) is decided client by client.

## What the AI sees

Only ledger names, groups and tags, never vouchers or amounts. With privacy mode on, party ledgers
show as codes.

## Tag what the books cannot show

Some tags are your knowledge, not in Tally: **related parties** and **MSME suppliers** in
particular. The audit checks for related parties and 43B(h) (MSME 45 days) only find what you
tag.

## Excel

To review in Excel instead:

```
tallyguru mapping export "Company name" mapping.csv
tallyguru mapping import "Company name" mapping.csv
```
