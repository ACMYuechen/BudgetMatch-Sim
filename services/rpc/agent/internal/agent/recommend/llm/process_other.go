//go:build !linux

package llm

import (
	"errors"
	"os/exec"
)

func confineMCPProcess(*exec.Cmd) error {
	return errors.New("MCP subprocesses require the supported Linux backend")
}
