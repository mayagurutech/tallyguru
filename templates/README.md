# Templates

Before the first sync of a company you confirm, by typing `I AGREE`, that you hold written
authorisation from the business:

    tallyguru authorise

It lists the companies open in TallyPrime; choose one by number.

- `authorisation_letter.md` — a letter the business can sign to authorise its accountant or
  auditor. Keeping a signed copy is strongly advised. Attaching it is optional:
  `tallyguru authorise "<company>" -letter <file>` (PDF/photo) or `-email <file>` (.eml / .msg).
- Business owners using their own books accept the owner's declaration instead:
  `tallyguru authorise "<company>" -owner`.

Authorisation must be re-confirmed in every financial year (April–March).

These templates are drafts and have not yet been reviewed by a lawyer. The tool cannot verify
any authorisation; legal responsibility stays with the user.
