# 1. Install and connect to Tally

You need Windows 10 or 11, TallyPrime, and an AI app that supports MCP (for example Claude
Desktop or Claude Code). The tool works on a 4 GB PC.

## Install

1. Run `TallyMCP-Setup-0.1.0.exe`. It does not ask for admin rights.
   - Windows may say "Windows protected your PC" (the program is not code-signed yet). Click
     **More info**, then **Run anyway**.
2. Read the licence and the "please read" page, then click **Next** and **Install**.
3. Leave **Open the control panel now** ticked and click **Finish**.

The control panel opens in your browser. It runs only on this PC and only while open. Open it
again later from the Start menu: **Tally MCP control panel**.

## First screen

Read the disclaimer and the data-protection notice, choose your language and your role (Owner,
Accountant or Auditor), tick **I have read the above and agree**, and click **Continue**.

## Turn on Tally's connection

In TallyPrime: **F1 (Help) > Settings > Connectivity**.
- **TallyPrime acts as:** Server (or Both).
- **Port:** 9000.

Restart TallyPrime if it asks. In the control panel's **Connection** section, click **Check
again**: "TallyPrime on localhost:9000" should turn green.

## Connect your AI app

In the **AI app** section:
- **Claude Desktop:** click **Show the change first** if you want to see it, then **Connect Claude
  Desktop** and **Yes, write it**. The old settings file is kept as a copy.
- **Claude Code or another MCP app:** copy the command shown and run it (Claude Code), or add a
  server with that command in your app's settings.

Then **fully quit** the AI app (for Claude Desktop: tray icon > Quit) and open it again. The tools
appear in the AI app's tool list.

## If something is red

Each red line in **Connection** says what to do. You can also run, in a Command Prompt:

```
"%LOCALAPPDATA%\Programs\TallyGuru\tallyguru.exe" doctor
```

See [9. Problems](09-problems.md).
