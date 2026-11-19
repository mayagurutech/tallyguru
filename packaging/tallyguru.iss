; Inno Setup script for Tally MCP by Guru (build with Inno Setup 7.1.1 or later: ISCC packaging\tallyguru.iss).
; Per-user install, no admin rights. The data folder is outside AppData (%USERPROFILE%\TallyGuru)
; and set in TALLYGURU_DATA, because the Microsoft Store Claude app redirects AppData: the AI
; app, the control panel and the command line must all use one folder.

#ifndef AppVersion
  #define AppVersion "0.1.0"
#endif
#define DataDir "{%USERPROFILE}\TallyGuru"

[Setup]
AppId={{6E0B3C3A-7A57-4C1E-9B49-6F1C9D5E2B11}
AppName=Tally MCP by Guru
AppVersion={#AppVersion}
AppPublisher=Mayank Agarwal
AppPublisherURL=https://github.com/mayagurutech/tallyguru
DefaultDirName={localappdata}\Programs\TallyGuru
DefaultGroupName=Tally MCP by Guru
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
UninstallDisplayName=Tally MCP by Guru

[Files]
Source: "..\dist\tallyguru.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "..\THIRD_PARTY_LICENSES"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
Name: "{#DataDir}"

[Registry]
; One data folder for everything (see the note at the top).
Root: HKCU; Subkey: "Environment"; ValueType: expandsz; ValueName: "TALLYGURU_DATA"; ValueData: "{#DataDir}"; Flags: uninsdeletevalue

[Icons]
Name: "{group}\Tally MCP control panel"; Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Comment: "Set up and manage Tally MCP by Guru"
Name: "{group}\Uninstall Tally MCP by Guru"; Filename: "{uninstallexe}"

[Run]
Filename: "{app}\tallyguru.exe"; Parameters: "-data ""{#DataDir}"" ui"; Description: "Open the control panel now"; Flags: postinstall nowait skipifsilent

[Code]
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
      if MsgBox('Also delete all client data stored by Tally MCP on this PC?' + #13#10 + Dir + #13#10#13#10 +
        'This removes the local copies of books, authorisation records, rules and drafts. Nothing in Tally is changed. This cannot be undone.',
        mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DelTree(Dir, True, True, True);
  end;
end;
