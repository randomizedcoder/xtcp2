package gotc

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// sysFS is the files tc reads besides netlink: /proc/net/psched
// (tc_core_init) and the class names databases (cls_names_init). Every
// path is read under root, so a replay or a test can stand in a directory
// for the host's /.
type sysFS struct {
	root string
	// usrDir is CONF_USR_DIR, where tc looks for tc_cls when
	// CONF_ETC_DIR has none. The flake builds it as iproute2's store
	// path's share/iproute2; neither holds a tc_cls in the workbench.
	usrDir string
}

// Where tc reads: tc_core.c's psched file and tc_util.c's NAMES_DB_ETC.
const (
	pschedPathCst  = "/proc/net/psched"
	confEtcDirCst  = "/etc/iproute2"
	confUsrDirCst  = "/usr/share/iproute2"
	clsNamesDBName = "tc_cls"
)

// hostSys is the host's files.
func hostSys() sysFS { return sysFS{root: "/", usrDir: confUsrDirCst} }

// path is p under root; a relative p (a -cf path) is taken from the
// working directory on the host, from root otherwise.
func (s sysFS) path(p string) string {
	if s.root == "/" || s.root == "" {
		return p
	}
	return filepath.Join(s.root, p)
}

// readFile is fopen and reading to the end. A directory opens, as fopen
// "r" opens one, and reads as empty, as fgets reads it (EISDIR).
func (s sysFS) readFile(p string) (string, error) {
	b, err := os.ReadFile(s.path(p))
	if errors.Is(err, unix.EISDIR) {
		return "", nil
	}
	return string(b), err
}

// tcCore is tc_core.c's clock_factor and tick_in_usec, 1 and 1 until
// tc_core_init reads the kernel's.
type tcCore struct {
	clockFactor float64
	tickInUsec  float64
}

// timeUnitsPerSec is TIME_UNITS_PER_SEC.
const timeUnitsPerSec = 1e6

// psched1G is the clock_res for which the kernel's t2us is a compat 1000.
const psched1G = 1000000000

// coreInit is tc_core_init: fscanf(fp, "%08x%08x%08x", &t2us, &us2t,
// &clock_res) of /proc/net/psched. A file it cannot open or scan leaves
// the defaults, silently. The kernel writes four words (t2us, us2t, 1e6,
// then the hrtimer resolution), so the third tc reads is 1000000, never
// the 1e9 the compat hack looks for.
func (s sysFS) coreInit() tcCore {
	c := tcCore{clockFactor: 1, tickInUsec: 1}
	content, err := s.readFile(pschedPathCst)
	if err != nil {
		return c
	}
	var v [3]uint32
	rest := content
	for i := range v {
		n, r, ok := scanHex(rest, 8)
		if !ok {
			return c
		}
		v[i], rest = n, r
	}
	t2us, us2t, clockRes := v[0], v[1], v[2]
	if clockRes == psched1G {
		t2us = us2t
	}
	c.clockFactor = float64(clockRes) / timeUnitsPerSec
	c.tickInUsec = float64(t2us) / float64(us2t) * c.clockFactor
	return c
}
