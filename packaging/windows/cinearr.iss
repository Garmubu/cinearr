; Windows installer of Cinearr (Inno Setup 6.3 or newer).
;
; Built by the release workflow:
;   iscc /DAppVersion=0.2.0 /DSourceDir=<folder with cinearr.exe, update.exe,
;        uninstall.exe and LICENSE> /DRepoURL=https://github.com/<owner>/<repo>
;        /O<output folder> packaging\windows\cinearr.iss
;
; - installs for the current user (no administrator rights), in the folder
;   of the user's choice, with a Start Menu shortcut and an optional desktop
;   shortcut;
; - refuses to run over an existing installation and offers update.exe or
;   uninstall.exe instead; update.exe runs it with /UPDATE to update;
; - uninstalling asks whether the data of Cinearr is deleted too
;   (uninstall.exe asks first and passes /DELETEDATA=yes|no).

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif
#ifndef SourceDir
  #define SourceDir "..\..\dist\windows-amd64"
#endif
#ifndef RepoURL
  #define RepoURL "https://github.com"
#endif

[Setup]
AppId={{8F0C1C2A-6B1E-4C1B-9E55-0C1F2A9D7E31}
AppName=Cinearr
AppVersion={#AppVersion}
AppVerName=Cinearr {#AppVersion}
AppPublisher=Cinearr contributors
AppPublisherURL={#RepoURL}
AppSupportURL={#RepoURL}/issues
AppUpdatesURL={#RepoURL}/releases
VersionInfoVersion={#AppVersion}
DefaultDirName={autopf}\Cinearr
DisableDirPage=no
DisableProgramGroupPage=yes
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
MinVersion=10.0
SetupIconFile=..\icons\cinearr.ico
UninstallDisplayIcon={app}\cinearr.exe
UninstallDisplayName=Cinearr
WizardStyle=modern
Compression=lzma2/max
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
ShowLanguageDialog=auto
OutputBaseFilename=Cinearr-Setup-{#AppVersion}

[Languages]
Name: "en"; MessagesFile: "compiler:Default.isl"
Name: "fr"; MessagesFile: "compiler:Languages\French.isl"
Name: "es"; MessagesFile: "compiler:Languages\Spanish.isl"
Name: "it"; MessagesFile: "compiler:Languages\Italian.isl"
Name: "de"; MessagesFile: "compiler:Languages\German.isl"

[CustomMessages]
en.AppComment=Find, download and watch films and series
fr.AppComment=Trouver, télécharger et regarder des films et des séries
es.AppComment=Buscar, descargar y ver películas y series
it.AppComment=Trovare, scaricare e guardare film e serie
de.AppComment=Filme und Serien finden, herunterladen und ansehen

en.AlreadyTitle=Cinearr is already installed
fr.AlreadyTitle=Cinearr est déjà installé
es.AlreadyTitle=Cinearr ya está instalado
it.AlreadyTitle=Cinearr è già installato
de.AlreadyTitle=Cinearr ist bereits installiert

en.AlreadyText=Cinearr %1 is installed in:%n%2%n%nTo update it, use:%n%3%n%nTo uninstall it (and reinstall it afterwards if you wish), use:%n%4
fr.AlreadyText=Cinearr %1 est installé dans :%n%2%n%nPour le mettre à jour, utilisez :%n%3%n%nPour le désinstaller (puis le réinstaller si vous le souhaitez), utilisez :%n%4
es.AlreadyText=Cinearr %1 está instalado en:%n%2%n%nPara actualizarlo, usa:%n%3%n%nPara desinstalarlo (y reinstalarlo después si quieres), usa:%n%4
it.AlreadyText=Cinearr %1 è installato in:%n%2%n%nPer aggiornarlo, usa:%n%3%n%nPer disinstallarlo (e reinstallarlo poi, se vuoi), usa:%n%4
de.AlreadyText=Cinearr %1 ist installiert in:%n%2%n%nZum Aktualisieren verwenden Sie:%n%3%n%nZum Deinstallieren (und danach bei Bedarf Neuinstallieren) verwenden Sie:%n%4

en.BtnUpdate=Update Cinearr
fr.BtnUpdate=Mettre à jour Cinearr
es.BtnUpdate=Actualizar Cinearr
it.BtnUpdate=Aggiorna Cinearr
de.BtnUpdate=Cinearr aktualisieren

en.BtnUninstall=Uninstall Cinearr
fr.BtnUninstall=Désinstaller Cinearr
es.BtnUninstall=Desinstalar Cinearr
it.BtnUninstall=Disinstalla Cinearr
de.BtnUninstall=Cinearr deinstallieren

en.AskDeleteData=Also delete the data of Cinearr: settings, list of films and series, history, logs and installed players?%n%nYes: delete everything.%nNo: keep the data for a later reinstallation.%n%nYour films, series and downloads are never deleted.
fr.AskDeleteData=Supprimer aussi les données de Cinearr : réglages, liste des films et des séries, historique, journaux et lecteurs installés ?%n%nOui : tout supprimer.%nNon : garder les données pour une prochaine installation.%n%nVos films, séries et téléchargements ne sont jamais supprimés.
es.AskDeleteData=¿Borrar también los datos de Cinearr: ajustes, lista de películas y series, historial, registros y reproductores instalados?%n%nSí: borrar todo.%nNo: conservar los datos para una reinstalación.%n%nTus películas, series y descargas nunca se borran.
it.AskDeleteData=Eliminare anche i dati di Cinearr: impostazioni, elenco di film e serie, cronologia, registri e lettori installati?%n%nSì: eliminare tutto.%nNo: conservare i dati per una reinstallazione.%n%nI tuoi film, le serie e i download non vengono mai eliminati.
de.AskDeleteData=Auch die Daten von Cinearr löschen: Einstellungen, Film- und Serienliste, Verlauf, Protokolle und installierte Player?%n%nJa: alles löschen.%nNein: Daten für eine spätere Neuinstallation behalten.%n%nIhre Filme, Serien und Downloads werden nie gelöscht.

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"

[Files]
Source: "{#SourceDir}\cinearr.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\update.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\uninstall.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#SourceDir}\LICENSE"; DestDir: "{app}"; DestName: "LICENSE.txt"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\Cinearr"; Filename: "{app}\cinearr.exe"; Comment: "{cm:AppComment}"
Name: "{autodesktop}\Cinearr"; Filename: "{app}\cinearr.exe"; Comment: "{cm:AppComment}"; Tasks: desktopicon

[Run]
Filename: "{app}\cinearr.exe"; Description: "{cm:LaunchProgram,Cinearr}"; Flags: nowait postinstall skipifsilent
; after an update made by update.exe (silent), Cinearr is started again
Filename: "{app}\cinearr.exe"; Flags: nowait; Check: IsUpdate

[UninstallDelete]
Type: dirifempty; Name: "{app}"

[Code]
const
  UninstallKey = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{8F0C1C2A-6B1E-4C1B-9E55-0C1F2A9D7E31}_is1';

var
  DeleteData: Integer; { 0: ask, 1: delete, 2: keep }

{ ParamValue returns the value of a command line option "/NAME=value"
  ("" when absent, "1" for "/NAME" alone). }
function ParamValue(const Name: String): String;
var
  I: Integer;
  P: String;
begin
  Result := '';
  for I := 1 to ParamCount do
  begin
    P := ParamStr(I);
    if CompareText(P, Name) = 0 then
    begin
      Result := '1';
      Exit;
    end;
    if CompareText(Copy(P, 1, Length(Name) + 1), Name + '=') = 0 then
    begin
      Result := Copy(P, Length(Name) + 2, MaxInt);
      Exit;
    end;
  end;
end;

{ IsUpdate: run by update.exe to replace the installed version. }
function IsUpdate(): Boolean;
begin
  Result := ParamValue('/UPDATE') <> '';
end;

{ ExistingInstall returns the folder of an installation still present. }
function ExistingInstall(var Version: String): String;
var
  Dir: String;
begin
  Result := '';
  if not RegQueryStringValue(HKCU, UninstallKey, 'InstallLocation', Dir) then
    if not RegQueryStringValue(HKLM, UninstallKey, 'InstallLocation', Dir) then
      Exit;
  if not RegQueryStringValue(HKCU, UninstallKey, 'DisplayVersion', Version) then
    RegQueryStringValue(HKLM, UninstallKey, 'DisplayVersion', Version);
  Dir := RemoveBackslashUnlessRoot(Dir);
  if FileExists(AddBackslash(Dir) + 'cinearr.exe') then
    Result := Dir;
end;

function InitializeSetup(): Boolean;
var
  Dir, Version, UpdateExe, UninstallExe: String;
  Answer, Code: Integer;
begin
  Result := True;
  if IsUpdate() then
    Exit;
  Dir := ExistingInstall(Version);
  if Dir = '' then
    Exit;
  UpdateExe := AddBackslash(Dir) + 'update.exe';
  UninstallExe := AddBackslash(Dir) + 'uninstall.exe';
  Answer := TaskDialogMsgBox(CustomMessage('AlreadyTitle'),
    FmtMessage(CustomMessage('AlreadyText'), [Version, Dir, UpdateExe, UninstallExe]),
    mbInformation, MB_YESNOCANCEL, [CustomMessage('BtnUpdate'), CustomMessage('BtnUninstall')], 0);
  if Answer = IDYES then
    ShellExec('', UpdateExe, '', Dir, SW_SHOWNORMAL, ewNoWait, Code)
  else if Answer = IDNO then
    ShellExec('', UninstallExe, '', Dir, SW_SHOWNORMAL, ewNoWait, Code);
  Result := False;
end;

function InitializeUninstall(): Boolean;
var
  Value: String;
begin
  Result := True;
  Value := Lowercase(ParamValue('/DELETEDATA'));
  if Value = 'yes' then
    DeleteData := 1
  else if Value = 'no' then
    DeleteData := 2
  else
    DeleteData := 0;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  Code: Integer;
begin
  if CurUninstallStep = usUninstall then
  begin
    if DeleteData = 0 then
    begin
      if MsgBox(CustomMessage('AskDeleteData'), mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES then
        DeleteData := 1
      else
        DeleteData := 2;
    end;
    { a running Cinearr keeps its program file open }
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM cinearr.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  end;
  if (CurUninstallStep = usPostUninstall) and (DeleteData = 1) then
  begin
    DelTree(ExpandConstant('{userappdata}\Cinearr'), True, True, True);
    DelTree(ExpandConstant('{app}\data'), True, True, True);
    RemoveDir(ExpandConstant('{app}'));
  end;
end;
