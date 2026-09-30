//go:build windows

package aria2

import (
	"os/exec"
	"syscall"
)

// configureProcess hides the console window aria2c would otherwise flash on
// startup, and disables Ctrl-C handling so the parent owns shutdown.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
