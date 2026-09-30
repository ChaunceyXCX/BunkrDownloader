//go:build !windows

package aria2

import (
	"os/exec"
	"syscall"
)

// configureProcess puts aria2c in its own process group so that Ctrl-C in the
// parent terminal does not race with our own shutdown, and so orphans are
// reaped with the group.
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
