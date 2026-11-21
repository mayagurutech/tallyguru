# 1. Install and connect to Tally

You need Windows 10 or 11, TallyPrime, and an AI app that supports MCP (for example Claude
Desktop or Claude Code). The tool works on a 4 GB PC.

## Install (three steps)

1. Run `TallyMCP-Setup-0.1.0.exe`. It does not ask for admin rights.
   - Windows may say "Windows protected your PC" (the program is not code-signed yet). Click
     **More info**, then **Run anyway**.
   - Read the licence and the "please read" page. Leave **Connect my AI app** ticked: setup adds
     the tool to Claude Desktop and Claude Code if they are on this PC, and keeps their old
     settings files as copies.
2. **Fully quit** your AI app and open it again (Claude Desktop: tray icon > Quit). AI apps load
   tools only when they start.
3. Open the company in TallyPrime, then ask the AI app: **Connect my Tally company**.

The AI app checks TallyPrime, shows you the data-protection notice and asks you to type
**I AGREE** once for the financial year (see [2. Your first company](02-first-company.md)), then
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

## The control panel (optional)

Start menu: **Tally MCP control panel**. It opens in your browser and is only needed for
settings: features and role, ledger mapping, drafts, deleting a company's local data, updates.
It runs in the background without a window and stops by itself when no panel page has been
open for 30 minutes.

## If something is wrong

Run, in a Command Prompt:

```
"%LOCALAPPDATA%\Programs\TallyGuru\tallyguru.exe" doctor
```

See [9. Problems](09-problems.md).
