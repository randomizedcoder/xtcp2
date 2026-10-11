package nlparity

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// A write row is compared differently from a read row, because only one side
// sends the write.
//
// `ip link set la1 master bond0` is three transactions on the wire: two
// RTM_GETLINK lookups (ll_name_to_index, master first, then the device) and
// the RTM_NEWLINK itself with its NLMSG_ERROR. goip sends the lookups, the
// same ones in the same order, and then writes the RTM_NEWLINK to the file
// GOIP_WRITE_OUT names instead of sending it (link-write-path.md,
// "Parity: write rows in the matrix"). So the goip capture holds one
// transaction fewer than ip's, and the request goip did not send is a file.
//
// The comparison splits ip's final transaction off, runs the ordinary Diff
// over the lookups, and compares ip's write request with goip's file at L2.
// It is request-only: goip cannot know whether the kernel would refuse the
// write, so the NLMSG_ERROR is not compared, and a refused row (enslave_up)
// passes on its request alone.

var (
	// ErrNoWriteTxn is an `ip` capture of a write row whose last transaction
	// is not an admitted write: the write was never sent, or something else
	// ran after it.
	ErrNoWriteTxn = errors.New("nlparity: the capture does not end in a write transaction")
	// ErrBadWriteFile is a GOIP_WRITE_OUT file that is not exactly one
	// admitted write request.
	ErrBadWriteFile = errors.New("nlparity: the write file is not one write request")
)

// SplitWrite separates an `ip` write capture into its lookups and its write.
//
// The write must be the last transaction, and the only write: iproute2 sends
// one per command. Notifications and orphans stay with the lookups, so
// HygieneDiff still sees them.
func SplitWrite(s Segmentation) (lookups Segmentation, write Txn, err error) {
	n := len(s.Txns)
	if n == 0 || !xtcpnl.IsAdmittedWriteType(s.Txns[n-1].Request.Hdr.Type) {
		return Segmentation{}, Txn{}, ErrNoWriteTxn
	}
	for i := range n - 1 {
		if xtcpnl.IsAdmittedWriteType(s.Txns[i].Request.Hdr.Type) {
			return Segmentation{}, Txn{}, fmt.Errorf("%w: transaction %d of %d is a write too",
				ErrNoWriteTxn, i, n)
		}
	}
	lookups = s
	lookups.Txns = append([]Txn(nil), s.Txns[:n-1]...)
	return lookups, s.Txns[n-1], nil
}

// ParseWriteRequest reads a GOIP_WRITE_OUT file: one netlink request whose
// nlmsg_len is the file's length, of an admitted write type.
func ParseWriteRequest(b []byte) (Msg, error) {
	var h xtcpnl.NlMsgHdr
	if _, err := xtcpnl.DeserializeNlMsgHdr(b, &h); err != nil {
		return Msg{}, fmt.Errorf("%w: %w", ErrBadWriteFile, err)
	}
	if int(h.Len) != len(b) {
		return Msg{}, fmt.Errorf("%w: nlmsg_len %d in a %d-byte file", ErrBadWriteFile, h.Len, len(b))
	}
	m := Msg{Hdr: h, Body: b[xtcpnl.NlMsgHdrSizeCst:]}
	if !m.IsRequest() || !xtcpnl.IsAdmittedWriteType(h.Type) {
		return Msg{}, fmt.Errorf("%w: %s flags 0x%04x", ErrBadWriteFile, MsgTypeName(h.Type), h.Flags)
	}
	return m, nil
}

// DiffWrite is Diff over the lookups plus L2 over the two write requests.
// The write is numbered after ref's lookups, where it sat in ref's capture.
func DiffWrite(command string, ref, sub Segmentation, refWrite, subWrite Msg) []Divergence {
	out := Diff(command, ref, sub)
	return append(out, diffRequest(command, len(ref.Txns), refWrite, subWrite)...)
}

// WriteControl is D_control for a write row: DiffWrite between the two `ip`
// captures, each split with SplitWrite.
func WriteControl(command string, ipA, ipB Segmentation) ([]Divergence, error) {
	a, aw, err := SplitWrite(ipA)
	if err != nil {
		return nil, fmt.Errorf("ip_a: %w", err)
	}
	b, bw, err := SplitWrite(ipB)
	if err != nil {
		return nil, fmt.Errorf("ip_b: %w", err)
	}
	return DiffWrite(command, a, b, aw.Request, bw.Request), nil
}

// CompareWrite is Compare for a write row. ref is the whole `ip` capture,
// write included; sub is goip's capture, which holds only lookups, and
// subWrite is the request goip wrote to GOIP_WRITE_OUT.
//
// Unlike Compare it accepts zero lookups on both sides, because `ip link
// add` resolves nothing. RefTxns counts ip's write, and SubTxns counts goip's
// file as its write, so a clean row reads as both sides running the same
// number of transactions.
func CompareWrite(command string, ref, sub Segmentation, subWrite Msg, control []Divergence, al *Allowlist) Report {
	r := Report{
		Command:     command,
		RefTxns:     len(ref.Txns),
		SubTxns:     len(sub.Txns) + 1,
		ControlSize: len(control),
		RefPids:     ref.Pids(),
		SubPids:     sub.Pids(),
	}
	if al != nil {
		r.Gated = al.IsGated(command)
	}

	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "ip", ref)...)
	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "goip", sub)...)

	lookups, write, err := SplitWrite(ref)
	if err != nil {
		r.Err = fmt.Errorf("reference capture: %w", err)
		return r
	}
	if sentWrite(sub) {
		r.Err = fmt.Errorf("subject capture: %w", ErrGoipSentWrite)
		return r
	}

	r.suppress(DiffWrite(command, lookups, sub, write.Request, subWrite), control, al)
	return r
}

// ErrGoipSentWrite is a goip capture that holds a write on the wire. goip
// never sends one, so this is a broken build, and it fails regardless of
// gating.
var ErrGoipSentWrite = errors.New("nlparity: goip sent a write request")

// sentWrite reports whether any transaction's request is a write.
func sentWrite(s Segmentation) bool {
	for i := range s.Txns {
		if xtcpnl.IsAdmittedWriteType(s.Txns[i].Request.Hdr.Type) {
			return true
		}
	}
	return false
}
