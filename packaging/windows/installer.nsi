; Quarel server installer for Windows (NSIS 3). Built by build.sh, which
; passes the product definitions below with -D.
;   NAME       display name          EXE      program file name
;   APPID      registry/mutex id     DIRNAME  folder under Program Files\Quarel
;   VERSION    version shown         NUMVER   numeric version (x.y.z.w)
;   SRC        folder with the files OUTFILE  installer to create
;   ICON       .ico                  LIVEKIT  defined when livekit-server.exe is bundled
;   DATADIR    data folder name under %LOCALAPPDATA%\Quarel

Unicode true
SetCompressor /SOLID lzma
!include "MUI2.nsh"

Name "${NAME}"
OutFile "${OUTFILE}"
InstallDir "$PROGRAMFILES64\Quarel\${DIRNAME}"
InstallDirRegKey HKLM "Software\Quarel\${APPID}" "InstallDir"
; Administrator rights: Program Files and firewall rules. The program itself
; then runs as the user, without elevation.
RequestExecutionLevel admin
BrandingText "Quarel ${VERSION} — logiciel libre (Apache-2.0)"

VIProductVersion "${NUMVER}"
VIAddVersionKey /LANG=1036 "ProductName" "${NAME}"
VIAddVersionKey /LANG=1036 "FileDescription" "Installation de ${NAME}"
VIAddVersionKey /LANG=1036 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=1036 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=1036 "LegalCopyright" "Les auteurs de Quarel — Apache-2.0"

!define MUI_ICON "${ICON}"
!define MUI_UNICON "${ICON}"
!define MUI_ABORTWARNING
!define MUI_WELCOMEPAGE_TITLE "Installation de ${NAME}"
!define MUI_WELCOMEPAGE_TEXT "${NAME} va être installé sur cet ordinateur.$\r$\n$\r$\nIl démarrera avec Windows et se placera près de l'horloge : un clic sur son icône ouvre la page d'administration dans votre navigateur.$\r$\n$\r$\nCliquez sur Suivant pour continuer."
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_TEXT "Lancer ${NAME} et ouvrir sa page d'administration"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAsUser
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "French"

!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APPID}"

; Started through Explorer so that it does not inherit the installer's
; administrator rights.
Function LaunchAsUser
  Exec '"$WINDIR\explorer.exe" "$INSTDIR\${EXE}"'
FunctionEnd

Section "Installer"
  ; An update: stop the running copy first.
  nsExec::Exec 'taskkill /IM "${EXE}" /F'
  !ifdef LIVEKIT
  nsExec::Exec 'taskkill /IM "livekit-server.exe" /F'
  !endif
  Sleep 800

  SetOutPath "$INSTDIR"
  File "${SRC}\${EXE}"
  !ifdef LIVEKIT
  File "${SRC}\livekit-server.exe"
  File "${SRC}\LICENSE-livekit.txt"
  !endif
  File "${SRC}\LICENSE.txt"
  File "${SRC}\NOTICE.txt"
  WriteUninstaller "$INSTDIR\Desinstaller.exe"

  CreateDirectory "$SMPROGRAMS\Quarel"
  CreateShortcut "$SMPROGRAMS\Quarel\${NAME}.lnk" "$INSTDIR\${EXE}"

  ; Start with the user's session (can be turned off from the tray menu).
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APPID}" '"$INSTDIR\${EXE}"'

  ; Incoming connections to the server programs (the administration page
  ; itself only listens on this machine).
  nsExec::Exec 'netsh advfirewall firewall delete rule name="${NAME}"'
  nsExec::Exec 'netsh advfirewall firewall add rule name="${NAME}" dir=in action=allow program="$INSTDIR\${EXE}" enable=yes profile=any'
  !ifdef LIVEKIT
  nsExec::Exec 'netsh advfirewall firewall delete rule name="${NAME} (vocal)"'
  nsExec::Exec 'netsh advfirewall firewall add rule name="${NAME} (vocal)" dir=in action=allow program="$INSTDIR\livekit-server.exe" enable=yes profile=any'
  !endif

  WriteRegStr HKLM "Software\Quarel\${APPID}" "InstallDir" "$INSTDIR"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayName" "${NAME}"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKLM "${UNINSTKEY}" "Publisher" "Quarel"
  WriteRegStr HKLM "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${EXE}"
  WriteRegStr HKLM "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Desinstaller.exe"'
  WriteRegStr HKLM "${UNINSTKEY}" "URLInfoAbout" "https://github.com/anlekg/quarel"
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${UNINSTKEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
  nsExec::Exec 'taskkill /IM "${EXE}" /F'
  !ifdef LIVEKIT
  nsExec::Exec 'taskkill /IM "livekit-server.exe" /F'
  !endif
  Sleep 800
  nsExec::Exec 'netsh advfirewall firewall delete rule name="${NAME}"'
  !ifdef LIVEKIT
  nsExec::Exec 'netsh advfirewall firewall delete rule name="${NAME} (vocal)"'
  !endif
  DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "${APPID}"
  Delete "$SMPROGRAMS\Quarel\${NAME}.lnk"
  RMDir "$SMPROGRAMS\Quarel"
  Delete "$INSTDIR\${EXE}"
  !ifdef LIVEKIT
  Delete "$INSTDIR\livekit-server.exe"
  Delete "$INSTDIR\LICENSE-livekit.txt"
  !endif
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\NOTICE.txt"
  Delete "$INSTDIR\Desinstaller.exe"
  RMDir "$INSTDIR"
  RMDir "$PROGRAMFILES64\Quarel"
  DeleteRegKey HKLM "${UNINSTKEY}"
  DeleteRegKey HKLM "Software\Quarel\${APPID}"
  ; The data (database, keys, settings, backups) is kept on purpose.
  MessageBox MB_OK "${NAME} a été désinstallé.$\r$\n$\r$\nSes données sont conservées dans :$\r$\n$LOCALAPPDATA\Quarel\${DATADIR}$\r$\n$\r$\nSupprimez ce dossier vous-même si vous n'en avez plus besoin (il contient les clés et la base du serveur)."
SectionEnd
