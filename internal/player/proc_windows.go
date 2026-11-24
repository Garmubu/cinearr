//go:build windows

package player

import (
	"os/exec"
	"syscall"
)

// hideConsole prevents a console window from flashing when a console
// program is started from the GUI build.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
