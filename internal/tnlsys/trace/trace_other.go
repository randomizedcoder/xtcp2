//go:build !(linux && amd64)

package trace

import (
	"errors"
	"os/exec"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

// Trace reads syscall registers, which only the x86_64 tracer knows.
func Trace(cmd *exec.Cmd) (*tnlsys.Run, []tnlsys.IoctlRec, error) {
	return nil, nil, errors.New("tnlsys: trace: only linux/amd64")
}
