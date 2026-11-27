package trace

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"golang.org/x/sys/unix"
)

// ifreqSize is sizeof(struct ifreq) on x86_64, and ifrDataOff the
// offset of ifr_data in it.
const (
	ifreqSize  = 40
	ifrDataOff = 16
)

// syscallStop is the stop signal PTRACE_O_TRACESYSGOOD gives a syscall
// stop.
const syscallStop = syscall.SIGTRAP | 0x80

// Trace runs cmd under ptrace and records every tunnel ioctl it makes:
// family (from the socket(2) the descriptor came from), request, device
// name and the argument before and after, with what the kernel wrote past
// it. The run's stdout, stderr and status come back in Run; cmd's own
// Stdout, Stderr and SysProcAttr are replaced. Threads are followed.
func Trace(cmd *exec.Cmd) (*tnlsys.Run, []tnlsys.IoctlRec, error) {
	type result struct {
		run  *tnlsys.Run
		recs []tnlsys.IoctlRec
		err  error
	}
	ch := make(chan result, 1)
	// Every ptrace request must come from the thread that started the
	// tracee: PTRACE_TRACEME makes that thread its tracer. The goroutine
	// keeps its thread locked to the end, so the thread exits with it and
	// never returns to the scheduler as some tracee's tracer.
	go func() {
		runtime.LockOSThread()
		r, recs, err := trace(cmd)
		ch <- result{r, recs, err}
	}()
	res := <-ch
	return res.run, res.recs, res.err
}

func trace(cmd *exec.Cmd) (*tnlsys.Run, []tnlsys.IoctlRec, error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("tnlsys: trace: %w", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		return nil, nil, joinClose(fmt.Errorf("tnlsys: trace: %w", err), outR, outW)
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	cmd.SysProcAttr = &syscall.SysProcAttr{Ptrace: true}
	if err := cmd.Start(); err != nil {
		return nil, nil, joinClose(fmt.Errorf("tnlsys: trace: %w", err), outR, outW, errR, errW)
	}
	if err := joinClose(nil, outW, errW); err != nil {
		return nil, nil, err
	}
	stdout, stderr := drain(outR), drain(errR)

	t := tracer{pid: cmd.Process.Pid, fam: map[int]int{}}
	rc, terr := t.run()
	run := &tnlsys.Run{Argv: append([]string(nil), cmd.Args...), RC: rc, Stdout: string(<-stdout), Stderr: string(<-stderr)}
	if cerr := joinClose(nil, outR, errR); terr == nil {
		terr = cerr
	}
	return run, t.recs, terr
}

// drain reads r to EOF on its own goroutine.
func drain(r io.Reader) <-chan []byte {
	ch := make(chan []byte, 1)
	go func() {
		var b bytes.Buffer
		if _, err := b.ReadFrom(r); err != nil {
			b.WriteString("\n[tnlsys: read: " + err.Error() + "]")
		}
		ch <- b.Bytes()
	}()
	return ch
}

// joinClose closes every file, joining their errors onto err.
func joinClose(err error, fs ...*os.File) error {
	for _, f := range fs {
		err = errors.Join(err, f.Close())
	}
	return err
}

// tracer is one traced process's state.
type tracer struct {
	pid  int
	fam  map[int]int // descriptor -> socket(2) domain, process-wide
	recs []tnlsys.IoctlRec
	thr  map[int]*thread
}

// thread is one traced thread's syscall state: entry and exit stops
// alternate per thread.
type thread struct {
	inSyscall bool
	nr        uint64
	sockDom   int
	pend      *pendingIoctl
}

// pendingIoctl is an ioctl seen at entry, waiting for its exit.
type pendingIoctl struct {
	family int
	req    uint32
	name   string
	arg    uintptr
	size   int
	in     []byte // size + tnlsys.SpillWindow bytes
}

// run steps the tracee's threads from syscall stop to syscall stop until
// the process exits, returning its exit status (128+N for a signal, as a
// shell reports it). Threads are followed (PTRACE_O_TRACECLONE) so a
// multithreaded command, a Go test helper say, is recorded whole.
func (t *tracer) run() (int, error) {
	var ws unix.WaitStatus
	if _, err := unix.Wait4(t.pid, &ws, 0, nil); err != nil {
		return -1, fmt.Errorf("tnlsys: trace: wait: %w", err)
	}
	if !ws.Stopped() {
		return -1, fmt.Errorf("tnlsys: trace: tracee did not stop at exec (%#x)", uint32(ws))
	}
	opts := unix.PTRACE_O_TRACESYSGOOD | unix.PTRACE_O_EXITKILL | unix.PTRACE_O_TRACECLONE
	if err := unix.PtraceSetOptions(t.pid, opts); err != nil {
		return -1, fmt.Errorf("tnlsys: trace: options: %w", err)
	}
	t.thr = map[int]*thread{t.pid: {}}
	if err := unix.PtraceSyscall(t.pid, 0); err != nil {
		return -1, fmt.Errorf("tnlsys: trace: resume: %w", err)
	}
	for {
		tid, err := unix.Wait4(-1, &ws, unix.WALL, nil)
		if err != nil {
			return -1, fmt.Errorf("tnlsys: trace: wait: %w", err)
		}
		if ws.Exited() || ws.Signaled() {
			delete(t.thr, tid)
			if tid == t.pid {
				return t.status(ws), nil
			}
			continue
		}
		sig := 0
		switch {
		case !ws.Stopped():
		case ws.StopSignal() == syscallStop:
			if err := t.syscallStop(tid); err != nil {
				if !threadGone(err) {
					return -1, err
				}
				delete(t.thr, tid)
				continue
			}
		case ws.StopSignal() == syscall.SIGTRAP:
			// PTRACE_EVENT_CLONE and the like: resume without a signal.
		case ws.StopSignal() == syscall.SIGSTOP && t.thr[tid] == nil:
			t.thr[tid] = &thread{} // a new thread's first stop
		default:
			sig = int(ws.StopSignal())
		}
		if err := unix.PtraceSyscall(tid, sig); err != nil && !errors.Is(err, unix.ESRCH) {
			return -1, fmt.Errorf("tnlsys: trace: resume %d: %w", tid, err)
		}
	}
}

// threadGone reports a ptrace request on a thread that no longer exists:
// ptrace(2) says a tracee can vanish at any stop, as a thread does when
// a sibling's exit_group kills it between the wait and the request. Its
// exit, if any, comes later from Wait4.
func threadGone(err error) bool {
	return errors.Is(err, unix.ESRCH)
}

// status is an exit status as a shell reports it.
func (t *tracer) status(ws unix.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ws.ExitStatus()
}

// syscallStop handles one entry or exit stop of thread tid.
func (t *tracer) syscallStop(tid int) error {
	th := t.thr[tid]
	if th == nil {
		th = &thread{}
		t.thr[tid] = th
	}
	var regs unix.PtraceRegs
	if err := unix.PtraceGetRegs(tid, &regs); err != nil {
		return fmt.Errorf("tnlsys: trace: regs: %w", err)
	}
	th.inSyscall = !th.inSyscall
	if th.inSyscall {
		th.nr = regs.Orig_rax
		return t.entry(th, tid, &regs)
	}
	return t.exit(th, tid, &regs)
}

func (t *tracer) entry(th *thread, tid int, regs *unix.PtraceRegs) error {
	switch th.nr {
	case unix.SYS_SOCKET:
		th.sockDom = int(cint.Low32(regs.Rdi))
	case unix.SYS_IOCTL:
		req := cint.Low32(regs.Rsi)
		if !tnlsys.KnownReq(req) {
			return nil
		}
		return t.ioctlEntry(th, tid, int(cint.Low32(regs.Rdi)), req, uintptr(regs.Rdx))
	}
	return nil
}

func (t *tracer) ioctlEntry(th *thread, tid int, fd int, req uint32, ifrAddr uintptr) error {
	ifr := make([]byte, ifreqSize)
	if err := t.peek(tid, ifrAddr, ifr); err != nil {
		return err
	}
	p := &pendingIoctl{
		family: t.fam[fd], req: req,
		name: tnlsys.IfName(string(ifr[:tnlsys.IfNameSize])),
		arg:  uintptr(binary.LittleEndian.Uint64(ifr[ifrDataOff:])),
	}
	switch req {
	case tnlsys.SIOCGETPRL:
		hdr := make([]byte, tnlsys.TunnelPRLSize)
		if err := t.peek(tid, p.arg, hdr); err != nil {
			return err
		}
		datalen := int(binary.LittleEndian.Uint32(hdr[8:12]))
		p.size = min(tnlsys.TunnelPRLSize+datalen, tnlsys.MaxArg)
	default:
		p.size = tnlsys.ArgSize(p.family, req)
	}
	p.in = make([]byte, p.size+tnlsys.SpillWindow)
	if err := t.peek(tid, p.arg, p.in); err != nil {
		return err
	}
	// Fill the window past the argument, as Live does, so that the exit
	// can tell the kernel's bytes from the tracee's own (its stack, whose
	// pointers vary run to run). ioctlExit restores what the kernel left.
	if err := t.poke(tid, p.arg+uintptr(p.size), bytes.Repeat([]byte{tnlsys.SpillFill}, tnlsys.SpillWindow)); err != nil {
		return err
	}
	th.pend = p
	return nil
}

func (t *tracer) exit(th *thread, tid int, regs *unix.PtraceRegs) error {
	errno, failed := retErrno(regs.Rax)
	switch th.nr {
	case unix.SYS_SOCKET:
		if !failed {
			t.fam[int(cint.Low32(regs.Rax))] = th.sockDom
		}
	case unix.SYS_IOCTL:
		p := th.pend
		th.pend = nil
		if p == nil {
			return nil
		}
		return t.ioctlExit(tid, p, errno)
	}
	return nil
}

// retErrno reads a syscall's return register: the kernel returns -errno,
// -4095 to -1, for a failure.
func retErrno(rax uint64) (unix.Errno, bool) {
	if rax < ^uint64(4095) {
		return 0, false
	}
	return unix.Errno(^rax + 1), true
}

func (t *tracer) ioctlExit(tid int, p *pendingIoctl, errno unix.Errno) error {
	out := make([]byte, len(p.in))
	if err := t.peek(tid, p.arg, out); err != nil {
		return err
	}
	// The kernel's write past the argument is a prefix of the window; the
	// tracee gets its own bytes back after it.
	spill := tnlsys.SpillOf(out[p.size:])
	if keep := p.size + len(spill); keep < len(p.in) {
		if err := t.poke(tid, p.arg+uintptr(keep), p.in[keep:]); err != nil {
			return err
		}
	}
	var s tnlsys.Snapshot
	if errno != 0 {
		s.AddIoctl(p.family, p.req, p.name, p.in[:p.size], nil, nil, errno)
	} else {
		s.AddIoctl(p.family, p.req, p.name, p.in[:p.size], out[:p.size], spill, 0)
	}
	t.recs = append(t.recs, s.Ioctls...)
	return nil
}

// poke writes b into thread tid's memory at addr.
func (t *tracer) poke(tid int, addr uintptr, b []byte) error {
	n, err := unix.PtracePokeData(tid, addr, b)
	if err != nil {
		return fmt.Errorf("tnlsys: trace: poke %#x: %w", addr, err)
	}
	if n != len(b) {
		return fmt.Errorf("tnlsys: trace: poke %#x: %d of %d bytes", addr, n, len(b))
	}
	return nil
}

// peek reads len(b) bytes of thread tid's memory at addr.
func (t *tracer) peek(tid int, addr uintptr, b []byte) error {
	n, err := unix.PtracePeekData(tid, addr, b)
	if err != nil {
		return fmt.Errorf("tnlsys: trace: peek %#x: %w", addr, err)
	}
	if n != len(b) {
		return fmt.Errorf("tnlsys: trace: peek %#x: %d of %d bytes", addr, n, len(b))
	}
	return nil
}
