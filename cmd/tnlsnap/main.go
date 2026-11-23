// Command tnlsnap records the tnlsys snapshot (internal/tnlsys) of one
// `ip` run: the tunnel ioctls it makes, traced under ptrace with their
// exact input and output; the tun properties of every device in
// /sys/class/net; and the /dev/net/tun descriptors in /proc.
//
//	tnlsnap -- -j tunnel show
//
// runs `ip -j tunnel show`, the `ip` first on PATH. It runs nothing but
// `ip`: it is a root ptrace tool in the capture guest, and its arguments
// come from a rows file.
//
// It prints the snapshot as JSON on stdout; the traced run's own stdout,
// stderr and exit status go into it rather than through. A capture row
// runs it as a readback beside the `ip` it captures, and goip replays the
// row with GOIP_TNLSYS naming the result. It exits 1 when the trace or a
// walk fails, and 2 on bad usage.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "--" {
		fmt.Fprintln(stderr, "usage: tnlsnap -- IP-ARG...")
		return 2
	}
	b, err := snapshotIP(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "tnlsnap: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(b); err != nil {
		fmt.Fprintf(stderr, "tnlsnap: %v\n", err)
		return 1
	}
	return 0
}

// snapshotIP records `ip ARGS`. The program is fixed, ip as PATH
// resolves it, and the arguments reach it as its argv, never a shell.
func snapshotIP(args []string) ([]byte, error) {
	path, err := exec.LookPath("ip")
	if err != nil {
		return nil, err
	}
	return snapshot(&exec.Cmd{Path: path, Args: append([]string{"ip"}, args...)}, tnlsys.Live{})
}

func snapshot(cmd *exec.Cmd, live tnlsys.Live) ([]byte, error) {
	r, recs, err := tnlsys.Trace(cmd)
	if err != nil {
		return nil, err
	}
	s := tnlsys.Snapshot{Ioctls: recs, Run: r}
	devs, err := netDevs(live)
	if err != nil {
		return nil, err
	}
	s.RecordProps(live, devs)
	fds, err := live.TunFDs()
	if err != nil {
		return nil, err
	}
	s.Fds = &fds
	return s.Marshal()
}

// netDevs lists /sys/class/net.
func netDevs(live tnlsys.Live) ([]string, error) {
	root := live.SysRoot
	if root == "" {
		root = "/sys"
	}
	ents, err := os.ReadDir(filepath.Join(root, "class", "net"))
	if err != nil {
		return nil, errors.Join(errors.New("listing devices"), err)
	}
	devs := make([]string, 0, len(ents))
	for _, e := range ents {
		devs = append(devs, e.Name())
	}
	return devs, nil
}
