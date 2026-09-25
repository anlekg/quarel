; Quarel desktop app installer additions (electron-builder NSIS).
; Before installing or updating, close a running Quarel so its files can be
; replaced. taskkill instead of electron-builder's PowerShell check: simpler,
; and it also works under Wine (used to test the installer on Linux).
!macro customCheckAppRunning
  nsExec::Exec `taskkill /F /IM "${APP_EXECUTABLE_FILENAME}"`
  Sleep 800
!macroend
