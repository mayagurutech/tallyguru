# 9. Problems

Start with the control panel's **Connection** section, or run `tallyguru doctor`. Each red line
says what to do.

| Problem | What to do |
|---|---|
| "Nothing is listening" on localhost:9000 | Open TallyPrime. **F1 > Settings > Connectivity**: TallyPrime acts as Server (or Both), port 9000. Restart TallyPrime. |
| "No company is open" | Open the company in TallyPrime (Alt+F3). |
| The AI app shows no Tally tools | Run `tallyguru connect` (or **AI app** in the control panel), then fully quit the AI app (tray icon > Quit) and reopen it. After changing Features, restart it again. |
| "No authorisation" | Ask the AI app to connect the company again and type I AGREE (each financial year), or use **Add a company** in the control panel. |
| The AI app opened the control panel instead of asking for I AGREE | That AI app cannot show forms. Record the authorisation in **Add a company**, then ask again. |
| Trial balance does not match | The sync checks itself first: it traces the differing ledgers month by month in Tally and reads those months again. If it still differs, the company card names the month, the difference and the likely voucher, and an Excel file "TB difference" lists them all. Answers stay blocked until it ties. Click **Full re-check** after correcting anything in Tally. Each differing ledger shows its group: a closing-stock ledger should be under Stock-in-Hand (renamed is fine). |
| Checking many clients at once | Open every client in TallyPrime and run `tallyguru doctor -tb`. It checks each authorised company and prints counts only (no names or amounts), safe to share when asking for help. |
| Sync is slow | The first sync reads every month once; later syncs read only changes. For very large companies run it from a Command Prompt with `-weekly`. Tally stays usable meanwhile. |
| "Writing to ... refused" | Only TEST MCP is writable by default. For a real client, type **ALLOW WRITES** in its **Drafts and writes** and restart the AI app. |
| "Windows protected your PC" when installing | The program isn't code-signed yet. Click **More info > Run anyway**. |
| A posted entry looks wrong | Ask the AI to cancel or delete it (needs the Alter/cancel/delete switch and the typed word), or correct it in Tally. The original is in the write log. |

## Reporting a problem

Run `tallyguru doctor` and include its output. Don't send company data or Excel files. Say what you
did and what you expected.
