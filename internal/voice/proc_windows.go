package voice

import (
	"os/exec"
	"syscall"
)

// hideWindow: livekit-server is a console program; started from the tray
// application it must not open a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
