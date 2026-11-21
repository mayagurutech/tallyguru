# 1. Install and connect to Tally

You need Windows 10 or 11, TallyPrime, and an AI app that supports MCP (for example Claude
Desktop or Claude Code). The tool works on a 4 GB PC.

## Install (three steps)

1. **Quit Claude first** (right-click the Claude icon near the clock > Quit; close Claude Code).
   Setup asks you to, because Claude would undo the connection if it stays open.
   Run `TallyMCP-Setup.exe`. It does not ask for admin rights.
   - Windows may say "Windows protected your PC" (the program is not code-signed yet). Click
     **More info**, then **Run anyway**.
   - Read the licence and the "please read" page. Leave **Connect Claude and keep it connected**
     ticked: setup adds the tool to Claude Code (even if you install or open Claude Code later)
     and to Claude Desktop if it is on this PC, and keeps their old settings files as copies.
     A small helper with no window (`tallyguru-agent.exe`, about 6 MB of memory) starts at
     Windows sign-in and checks every 2 minutes that Claude still has the tool; it reads and
     sends nothing else. If Claude ever loses the tool, just restart Claude a few minutes later.
2. **Fully quit** your AI app and open it again (Claude Desktop: tray icon > Quit). AI apps load
   tools only when they start.
3. Open the company in TallyPrime, then ask the AI app: **Connect my Tally company**.

The AI app checks TallyPrime. Once per financial year a Windows box with the data-protection
notice appears on top of Claude: read it and click **Yes** (see [2. Your first company](02-first-company.md)), then
starts the first sync. You can ask questions as soon as it finishes. From then on, open any
company in TallyPrime and just ask: Claude sees which company is open and connects a new one
by itself. In Claude Code, setup also lets the reading tools run without asking each time;
posting to Tally still asks.

## If TallyPrime does not answer

The AI app tells you what to change. In TallyPrime: **F1 (Help) > Settings > Connectivity**.
- **TallyPrime acts as:** Server (or Both).
- **Port:** 9000.

Restart TallyPrime if it asks, then ask the AI app again.

## Other AI apps

If your AI app was not connected by setup, run this once in a Command Prompt, or add a server
with the command `tallyguru.exe -data "%USERPROFILE%\TallyGuru" serve` in your app's settings:

```
"%LOCALAPPDATA%\Programs\TallyGuru\tallyguru.exe" connect
```

## Everything is on, and it updates itself

Every feature is switched on for everyone: reports, audit checks, mapping, voucher entry,
changes, bulk import and bank statements. Posting to a real company still asks you once per
company (a Windows box: click Yes), and every entry is shown to you before it is posted. To
switch features off, use **Features** in the control panel.

The tool keeps itself up to date. The small helper checks GitHub for a newer release 10 minutes
after you sign in and then every 6 hours. It installs it quietly, but only when Claude is not
using the tool (if Claude is open, it tries again later). After an update, the next time you
open Claude it uses the new version. Only public release files are downloaded; nothing about you
or your books is sent.

## The control panel (optional)

Start menu: **LedgerGuru control panel**. It opens in your browser and is only needed for
settings: features and role, ledger mapping, drafts, deleting a company's local data, updates.
It runs in the background without a window and stops by itself when no panel page has been
open for 30 minutes.

## If something is wrong

Run, in a Command Prompt:

```
"%LOCALAPPDATA%\Programs\TallyGuru\tallyguru.exe" doctor
```

See [9. Problems](09-problems.md).
