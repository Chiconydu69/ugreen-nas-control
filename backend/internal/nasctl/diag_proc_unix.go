//go:build unix

package nasctl

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the child in its own process group and kills the whole group on timeout, so a tool that forked
// helpers leaves nothing behind.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}
