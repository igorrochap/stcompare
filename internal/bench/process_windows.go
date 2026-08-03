//go:build windows

package bench

import (
	"os"
	"os/exec"
)

func configureProcessGroup(_ *exec.Cmd) {}

func killProcessGroup(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	return process.Kill()
}
