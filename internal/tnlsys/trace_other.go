//go:build !(linux && amd64)

package tnlsys

import (
	"errors"
	"os/exec"
)

// Trace reads syscall registers, which only the x86_64 tracer knows.
func Trace(cmd *exec.Cmd) (*Run, []IoctlRec, error) {
	return nil, nil, errors.New("tnlsys: trace: only linux/amd64")
}
