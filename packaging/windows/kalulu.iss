; Inno Setup script of the Kalulu installer. build.sh copies it into the
; work directory next to version.iss and the stage folder, then compiles it.
; It installs Kalulu, its codec libraries and the tools of playback
; (yt-dlp, ffmpeg and ffprobe in {app}\tools), for the current user by
; default, so the bundled yt-dlp can update itself.

#include "version.iss"

[Setup]
AppId={{E6E04716-7728-4DC3-88BE-4128BC16A3C3}
AppName=Kalulu
AppVersion={#AppVersion}
AppVerName=Kalulu {#AppVersion}
AppPublisher=Kalulu
AppPublisherURL=https://github.com/HubDevCode/Kalulu
AppSupportURL=https://github.com/HubDevCode/Kalulu/issues
AppUpdatesURL=https://github.com/HubDevCode/Kalulu/releases
VersionInfoVersion={#NumVersion}
DefaultDirName={autopf}\Kalulu
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
LicenseFile=stage\licenses\LICENSE.txt
SetupIconFile=kalulu.ico
UninstallDisplayIcon={app}\kalulu.exe
UninstallDisplayName=Kalulu
WizardStyle=modern
Compression=lzma2/ultra64
SolidCompression=yes
CloseApplications=force
RestartApplications=no
OutputDir=.
OutputBaseFilename=Kalulu-{#AppTag}-x64-setup

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"
Name: "italian"; MessagesFile: "compiler:Languages\Italian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: unchecked

[InstallDelete]
; A new yt-dlp comes with the update: let Kalulu check it again.
Type: files; Name: "{app}\tools\.yt-dlp-updated"

[Files]
Source: "stage\kalulu.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "stage\*.dll"; DestDir: "{app}"; Flags: ignoreversion
Source: "stage\tools\*"; DestDir: "{app}\tools"; Flags: ignoreversion recursesubdirs
Source: "stage\licenses\*"; DestDir: "{app}\licenses"; Flags: ignoreversion recursesubdirs

[Icons]
Name: "{autoprograms}\Kalulu"; Filename: "{app}\kalulu.exe"
Name: "{autodesktop}\Kalulu"; Filename: "{app}\kalulu.exe"; Tasks: desktopicon

[Run]
Filename: "{app}\kalulu.exe"; Description: "{cm:LaunchProgram,Kalulu}"; Flags: nowait postinstall skipifsilent
; The in-app updater runs the installer silently: start Kalulu again.
Filename: "{app}\kalulu.exe"; Flags: nowait; Check: WizardSilent

[UninstallDelete]
; yt-dlp -U leaves files that the installer did not put there.
Type: filesandordirs; Name: "{app}\tools"
