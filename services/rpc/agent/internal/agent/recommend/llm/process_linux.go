//go:build linux

package llm

import (
	"os"
	"os/exec"
	"syscall"
)

// confineMCPProcess 仅约束子进程生命周期，不是文件系统或网络沙箱。
func confineMCPProcess(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return nil
}
