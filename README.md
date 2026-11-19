# Tally MCP by Guru

A free, open-source MCP server that assists accountants, auditors and business owners with
accounting, reconciliation, audit checks and reports on TallyPrime data, through any
MCP-capable AI app (Claude Desktop, Claude Code and others).

> **Status:** pre-release (0.1.0-dev). Tested by the author on his own clients' data and on a
> dummy company. Public beta comes next.

**Not affiliated with or endorsed by Tally Solutions Pvt. Ltd. Tally and TallyPrime are
trademarks of Tally Solutions Pvt. Ltd.** "TallyPrime" is used only to describe compatibility.

## What it does

- **Reads your books safely.** It reads TallyPrime month by month, one small request at a time,
  into a local database on your PC, so Tally doesn't hang even on large companies. It checks the
  trial balance against Tally after every sync.
- **Answers with few tokens.** The server does the maths and sends the AI short tables, not raw
  entries. Full detail goes to Excel on your PC.
- **Reports:** trial balance, ledgers, outstanding, P&L and Balance Sheet (Tally and Schedule III
  layouts), ratios, cash flow, owner MIS, GST and TDS summaries, ageing, Day Book.
- **Audit checks on every entry** (facts for the auditor to judge, not conclusions): cash over the
  limits (Income-tax Act 2025 sections, with the 1961 section in brackets), MSME 45 days, TDS not
  deducted, related parties, negative cash, suspense, duplicates, round sums, Sunday entries, old
  debtors, invalid GSTINs, IGST vs state, foreign parties. One Excel file with Summary, Issues and
  Working.
- **Entry, only when you switch it on:**
  - every voucher type (accounting and item invoice);
  - masters;
  - alter, cancel and delete;
  - bulk import of entries the AI reads from your files;
  - bank statement checks, matching and the bank reconciliation statement;
  - a per-client memory of your own ledger rules.
- **One control panel** (a local page in your browser) for setup, connection, features, companies,
  sync, mapping, drafts and rules.

## What it never does

- It sends nothing to the author or to any cloud. There is no telemetry. The only outside request
  is the optional "check for a new version" button, which reads GitHub's public release page.
- It reads only Tally. Portal files and statements are read by your AI app, not by this tool.
- It never posts an entry without a preview that you confirm. Alter, cancel and delete need a typed
  word. Writing to a real client is off until you allow it for that company.

## Install (Windows)

1. Download `TallyMCP-Setup.exe` from Releases and run it. It doesn't need admin rights.
2. The control panel opens. Read and accept the notice, and pick your role.
3. In TallyPrime: F1 (Help) > Settings > Connectivity, set "TallyPrime acts as" to Server (or
   Both), port 9000. The panel's Connection section shows a green mark when it works.
4. AI app: click **Connect Claude Desktop**, or copy the command shown for Claude Code or another
   app. Then fully quit and reopen the AI app.
5. Add a company: open it in TallyPrime, record your authorisation in the panel, and click
   **Sync now**.

Step-by-step guides: [docs/guide](docs/guide/README.md).

You can open the panel again any time from the Start menu: **Tally MCP control panel**.
For support, run `tallyguru doctor`: it checks Tally, the data folder and the AI app connection.

## Roles

| Role | Loads | Writes |
|---|---|---|
| Owner | core, reports | off |
| Accountant | core, mapping, reports, voucher entry, masters | on, after previews |
| Auditor | core, mapping, reports, audit checks | off |

Alter/cancel/delete, bulk import and bank statements are separate switches, off by default (they
add to the AI's tool list).

## How light is it (measured on the author's pilot, not a guarantee)

| | Result |
|---|---|
| Memory | 15–20 MB idle; 38 MB peak during a full sync of a large company |
| Answers | under 1 second after sync; full audit of six months in about 0.5 s |
| Tokens | tool definitions about 750–1,500 per role; a full audit answer about 460 tokens |
| Posting | 1,000 vouchers in about 64 seconds (test company) |

## Disclaimer

This software is provided "as is" under the MIT licence, without warranty of any kind. It assists
with accounting, reconciliation, audit checks and reports; it is not professional, tax or legal
advice. Back up your Tally data before any entry is posted. Check every output yourself. Audit
findings need the auditor's own judgment and sign-off. You are responsible for your clients' data
and for holding their authorisation. The Digital Personal Data Protection Act, 2023 applies to
personal data in the books. Data the AI reads is sent to your AI service provider for processing.

## Code signing policy

Code signing has been applied for with SignPath Foundation (free code signing for open-source
projects). Until it is granted, releases are unsigned and Windows shows a warning on install.

- Every release file is built from this repository's source by GitHub Actions
  ([release.yml](.github/workflows/release.yml)); nothing is built or signed on a personal PC.
- Committers and reviewers: [Mayank Agarwal](https://github.com/mayagurutech). Changes from anyone
  else (pull requests) are reviewed by him before merging.
- Approvers: [Mayank Agarwal](https://github.com/mayagurutech). Every release is approved by hand
  before it is signed.
- Privacy: this program will not transfer any information to other networked systems unless
  specifically requested by the user or the person installing or operating it. It talks to
  TallyPrime on your own PC (localhost). The only outside request is the optional "check for a new
  version" button, which reads GitHub's public release page. See [Privacy](docs/guide/08-privacy.md).
- System changes: the installer installs for the current user only (no admin rights), adds a
  Start-menu shortcut, and sets one user environment variable, `TALLYGURU_DATA`, pointing to the data
  folder `%USERPROFILE%\TallyGuru`. "Connect Claude Desktop" changes the AI app's settings file only
  when you click it, and keeps a backup. Uninstall from Windows Settings > Apps; it asks before
  deleting client data.

## Credits

Ideas inspired by Dhananjay Gokhale's
[tally-mcp-server](https://github.com/dhananjay1405/tally-mcp-server) (MIT). This project is a
fresh implementation, not a fork.

## Licence

MIT. See [LICENSE](LICENSE). Third-party licences: [THIRD_PARTY_LICENSES](THIRD_PARTY_LICENSES).
