; Inno Setup script for LedgerGuru (build with Inno Setup 7.1.1 or later: ISCC packaging\tallyguru.iss).
; Per-user install, no admin rights. The data folder is outside AppData (%USERPROFILE%\TallyGuru)
; and set in TALLYGURU_DATA, because the Microsoft Store Claude app redirects AppData: the AI
; app, the control panel and the command line must all use one folder.

#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif
#define DataDir "{%USERPROFILE}\TallyGuru"

[Setup]
AppId={{6E0B3C3A-7A57-4C1E-9B49-6F1C9D5E2B11}
AppName=LedgerGuru – MCP for TallyPrime
AppVersion={#AppVersion}
AppPublisher=Mayank Agarwal
AppPublisherURL=https://github.com/mayagurutech/tallyguru
DefaultDirName={localappdata}\Programs\TallyGuru
DefaultGroupName=LedgerGuru
; Renamed from "Tally MCP by Guru" (21 Nov 2026): upgrades move to the new Start-menu folder.
UsePreviousGroup=no
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
OutputDir=..\dist
OutputBaseFilename=TallyMCP-Setup-{#AppVersion}
LicenseFile=..\LICENSE
InfoBeforeFile=DISCLAIMER.txt
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
ChangesEnvironment=yes
; Updates from the control panel run this quietly while the panel and the AI app's server are open:
; close them, then reopen the panel.
CloseApplications=force
RestartApplications=no
UninstallDisplayName=LedgerGuru – MCP for TallyPrime

[Files]
Source: "..\dist\tallyguru.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\dist\tallyguru-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\THIRD_PARTY_LICENSES"; DestDir: "{app}"; Flags: ignoreversion

[InstallDelete]
; Shortcuts under the old name (before 21 Nov 2026).
Type: filesandordirs; Name: "{userprograms}\Tally MCP by Guru"
Type: files; Name: "{userdesktop}\Tally MCP control panel.lnk"

[Dirs]
Name: "{#DataDir}"

[Registry]
; One data folder for everything (see the note at the top).
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "TALLYGURU_DATA"; ValueData: "{#DataDir}"; Flags: uninsdeletevalue
; Keeps the AI apps connected: a small helper with no window starts at sign-in and every two
; minutes re-adds the tool if Claude lost it (Claude installed later, or old settings written back).
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "TallyGuruAgent"; ValueData: """{app}\tallyguru-agent.exe"" -data ""{#DataDir}"""; Flags: uninsdeletevalue; Tasks: connectai

[Icons]
Name: "{group}\LedgerGuru control panel"; Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Comment: "Set up and manage LedgerGuru"
Name: "{group}\Uninstall LedgerGuru"; Filename: "{uninstallexe}"
; Also on the desktop: Windows search does not always find a new Start-menu entry at once.
Name: "{userdesktop}\LedgerGuru control panel"; Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Comment: "Set up and manage LedgerGuru"

[Tasks]
Name: "connectai"; Description: "Connect Claude and keep it connected (Claude Code, and Claude Desktop if found; a small helper with no window checks every 2 minutes). Old settings files are kept as copies."

[Run]
; Setup is finished from the AI app's chat: connect the AI app here, then the user asks it to connect a company.
Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" connect"; StatusMsg: "Connecting your AI app..."; Flags: runhidden waituntilterminated; Tasks: connectai
Filename: "{app}\tallyguru-agent.exe"; Parameters: "-data ""{#DataDir}"""; Flags: nowait runhidden; Tasks: connectai
Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Description: "Also open the control panel (settings; optional)"; Flags: postinstall nowait skipifsilent unchecked
Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Flags: nowait; Check: WizardSilent and not AutoUpdate

[UninstallRun]
; Stop the helper before its file is removed.
Filename: "{sys}\taskkill.exe"; Parameters: "/F /IM tallyguru-agent.exe"; Flags: runhidden; RunOnceId: "StopAgent"

[Messages]
FinishedLabel=LedgerGuru is installed.%n%nNext:%n1. In TallyPrime, open the company you want.%n2. Quit Claude fully (tray icon > Quit) and open it again.%n3. Ask Claude: Connect my Tally company.%n%nThe control panel (Start menu) is only needed for settings.

[Code]
// Claude rewrites its settings files when it closes, which would undo "Connect my AI app" if it
// is running during setup (seen on a new PC): ask the user to quit it first.
function ClaudeRunning(): Boolean;
var
  Locator, Svc, Found: Variant;
begin
  Result := False;
  try
    Locator := CreateOleObject('WbemScripting.SWbemLocator');
    Svc := Locator.ConnectServer('.', 'root\CIMV2');
    Found := Svc.ExecQuery('SELECT Name FROM Win32_Process WHERE Name = ''claude.exe'' OR Name = ''Claude.exe''');
    Result := Found.Count > 0;
  except
    Result := False;
  end;
end;

// Automatic updates by the background helper pass /auto=1: no control panel afterwards.
function AutoUpdate(): Boolean;
begin
  Result := ExpandConstant('{param:auto|0}') = '1';
end;

function NextButtonClick(CurPageID: Integer): Boolean;
begin
  Result := True;
  if (CurPageID = wpReady) and WizardIsTaskSelected('connectai') then
    while ClaudeRunning() do
      if MsgBox('Please quit Claude first: right-click the Claude icon near the clock and choose Quit, and close Claude Code.' + #13#10#13#10 +
        'Setup adds this tool to Claude''s settings; Claude would undo it if it stays open.' + #13#10#13#10 +
        'Click Retry when Claude is closed.', mbInformation, MB_RETRYCANCEL) = IDCANCEL then
      begin
        Result := False;
        Exit;
      end;
end;

// Uninstall removes the program. Client data (books, authorisation records, rules) is deleted
// only if the user says so; it can also be deleted per client from the control panel.
procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Dir: string;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    Dir := ExpandConstant('{#DataDir}');
    if DirExists(Dir) then
      if MsgBox('Also delete all client data stored by LedgerGuru on this PC?' + #13#10 + Dir + #13#10#13#10 +
        'This removes the local copies of books, authorisation records, rules and drafts. Nothing in Tally is changed. This cannot be undone.',
        mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(Dir, True, True, True);
  end;
end;
