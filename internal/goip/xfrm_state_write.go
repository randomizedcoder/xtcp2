package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip xfrm state`'s writes (ip/xfrm_state.c):
//   - add and update, xfrm_state_modify: one NEWSA or UPDSA, struct
//     xfrm_usersa_info and the attributes its words give, with NLM_F_ACK;
//   - allocspi, xfrm_state_allocspi: one ALLOCSPI, struct
//     xfrm_userspi_info, which the kernel answers with the larval SA;
//   - delete, xfrm_state_get_or_delete: get's request as a DELSA with
//     NLM_F_ACK;
//   - deleteall, xfrm_state_list_or_deleteall: GETSA dumps, each answer the
//     filter passes copied into a DELSA, the copies sent in one datagram
//     with no ACK asked for, until a dump finds none;
//   - flush, xfrm_state_flush: one FLUSHSA, struct xfrm_usersa_flush.
//
// goip sends the dumps and writes every write to GOIP_WRITE_OUT, as ip
// sends it. It sends no write, so allocspi has no answer to print, where ip
// prints the larval SA, and deleteall's next dump still finds what goip
// copied: goip skips an SA it has copied, and ends after the round that
// copies none, the rounds ip runs when the kernel deletes what it is asked.

const (
	// xfrmRtaBufCst is RTA_BUF_SIZE, the bound addattr_l holds a write to.
	xfrmRtaBufCst = 2048
	// xfrmAlgoKeyBufCst is XFRM_ALGO_KEY_BUF_SIZE, the longest key.
	xfrmAlgoKeyBufCst = 512
	// xfrmCtxBufCst is CTX_BUF_SIZE, ctx's buffer, which ip overruns.
	xfrmCtxBufCst = 256
	// xfrmDeleteallBufCst is NLMSG_DELETEALL_BUF_SIZE, deleteall's buffer.
	xfrmDeleteallBufCst = 8192
	// xfrmReplayESNMaxCst is XFRMA_REPLAY_ESN_MAX.
	xfrmReplayESNMaxCst = 4096
	// xfrmReplayBitmapBitsCst is struct xfrm_replay_state's bitmap's bits.
	xfrmReplayBitmapBitsCst = 32
	// xfrmAlgoNameLenCst is alg_name's 64 bytes.
	xfrmAlgoNameLenCst = 64
	xfrmStateESNCst    = 128 // XFRM_STATE_ESN
	xfrmSADirInCst     = 1   // XFRM_SA_DIR_IN
	xfrmSADirOutCst    = 2   // XFRM_SA_DIR_OUT
	xfrmOffloadInCst   = 2   // XFRM_OFFLOAD_INBOUND
	xfrmOffloadPktCst  = 4   // XFRM_OFFLOAD_PACKET
	xfrmModeBeetCst    = 4   // XFRM_MODE_BEET
	xfrmModeROCst      = 2   // XFRM_MODE_ROUTEOPTIMIZATION
	xfrmModeTrigCst    = 3   // XFRM_MODE_IN_TRIGGER
	xfrmPcpuUnsetCst   = 0xffffffff
	// xfrmMarkLenCst is struct xfrm_mark's 8 bytes.
	xfrmMarkLenCst   = 8
	xfrmAddCst       = "add"
	xfrmUpdateCst    = "update"
	xfrmAllocspiCst  = "allocspi"
	xfrmDeleteallCst = "deleteall"
	xfrmFlushCst     = "flush"
	xfrmDirCst       = "dir"
	xfrmDirOutCst    = "out"
	xfrmPcpuCst      = "pcpu-num"
	xfrmSeqCst       = "seq"
	xfrmMaskWordCst  = "mask"
)

// xfrmAddattr is a request ip builds in a fixed buffer: nlLen is
// nlmsg_len, and each attribute is addattr_l's, refused, with its message,
// when it would take the message past maxLen.
type xfrmAddattr struct {
	c      *runCtx
	nlLen  int
	maxLen int
	attrs  []byte
}

// add is addattr_l: false, and the message on stderr, when the attribute
// does not fit.
func (r *xfrmAddattr) add(typ uint16, data []byte) (bool, error) {
	l := 4 + len(data)
	if nlmsgAlign(r.nlLen)+nlmsgAlign(l) > r.maxLen {
		_, err := fmt.Fprintf(r.c.errOut, "addattr_l ERROR: message exceeded bound of %d\n", r.maxLen)
		return false, err
	}
	r.attrs = append(r.attrs, make([]byte, nlmsgAlign(r.nlLen)-r.nlLen)...)
	r.attrs = binary.LittleEndian.AppendUint16(r.attrs, uint16(l))
	r.attrs = binary.LittleEndian.AppendUint16(r.attrs, typ)
	r.attrs = append(r.attrs, data...)
	r.attrs = append(r.attrs, make([]byte, nlmsgAlign(l)-l)...)
	r.nlLen = nlmsgAlign(r.nlLen) + nlmsgAlign(l)
	return true, nil
}

// add32 is addattr32.
func (r *xfrmAddattr) add32(typ uint16, v uint32) error {
	_, err := r.add(typ, binary.LittleEndian.AppendUint32(nil, v))
	return err
}

// xfrmSA is struct xfrm_usersa_info as xfrm_state_modify fills it.
type xfrmSA struct {
	sel                  xfrmSel
	id                   xfrmID
	lft                  [8]uint64
	seq, reqid           uint32
	family               uint16
	mode, window, sflags uint8
}

// bytes is the SA's 224 bytes; its current lifetime and statistics are
// zero.
func (s *xfrmSA) bytes() []byte {
	b := make([]byte, xtcpnl.XfrmUsersaInfoSizeCst)
	copy(b, s.sel.bytes())
	copy(b[56:], s.id.daddr[:])
	binary.BigEndian.PutUint32(b[72:], s.id.spi)
	b[76] = s.id.proto
	copy(b[80:], s.id.saddr[:])
	for i, v := range s.lft {
		binary.LittleEndian.PutUint64(b[96+8*i:], v)
	}
	binary.BigEndian.PutUint32(b[204:], s.seq)
	binary.LittleEndian.PutUint32(b[208:], s.reqid)
	binary.LittleEndian.PutUint16(b[212:], s.family)
	b[214], b[215], b[216] = s.mode, s.window, s.sflags
	return b
}

// xfrmModify is xfrm_state_modify's state: the SA, the attributes in
// argument order, and what its loop reads for the checks after it.
type xfrmModify struct {
	c                              *runCtx
	sa                             xfrmSA
	req                            xfrmAddattr
	window, rseq, roseq            uint32
	rseqHi, roseqHi                uint32
	ifindex                        int32
	dir                            uint8
	offload, packet                bool
	id, aead, enc, auth, comp, coa bool
	ctx                            bool
	extraFlags                     uint32
	mark, outputMark               [2]uint32
	ifID, tfcpad, pcpu             uint32
	ifIDSet                        bool
}

// xfrmModifyWord is one of the loop's words: it reads its values from a,
// whose cur is the word.
type xfrmModifyWord func(m *xfrmModify, a *xfrmArgs) error

// xfrmModifyWords are the loop's words by strcmp; any other is an
// algorithm type or the ID.
var xfrmModifyWords = map[string]xfrmModifyWord{
	xfrmModeCst:      func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextMode(a, &m.sa.mode) },
	xfrmMarkCst:      func(m *xfrmModify, a *xfrmArgs) error { return xfrmParseMark(a, &m.mark) },
	xfrmReqidCst:     func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextReqid(a, &m.sa.reqid) },
	xfrmSeqCst:       func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextSeq(a, &m.sa.seq) },
	"replay-window":  func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.window) },
	"replay-seq":     func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.rseq) },
	"replay-seq-hi":  func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.rseqHi) },
	"replay-oseq":    func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.roseq) },
	"replay-oseq-hi": func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.roseqHi) },
	xfrmFlagCst: func(m *xfrmModify, a *xfrmArgs) error {
		if err := a.next(); err != nil {
			return err
		}
		return xfrmStateFlagParse(a, &m.sa.sflags)
	},
	"extra-flag": func(m *xfrmModify, a *xfrmArgs) error { return xfrmExtraFlagParse(a, &m.extraFlags) },
	"sel":        (*xfrmModify).selector,
	"limit":      func(m *xfrmModify, a *xfrmArgs) error { return xfrmLifetimeParse(a, &m.sa.lft) },
	l2tpEncapCst: (*xfrmModify).encap,
	"coa":        (*xfrmModify).careOf,
	"ctx":        (*xfrmModify).secCtx,
	"offload":    (*xfrmModify).offloadDev,
	"output-mark": func(m *xfrmModify, a *xfrmArgs) error {
		return xfrmOutputMarkParse(a, &m.outputMark)
	},
	"if_id": func(m *xfrmModify, a *xfrmArgs) error {
		m.ifIDSet = true
		return xfrmNextU32(a, &m.ifID)
	},
	"tfcpad":    func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.tfcpad) },
	xfrmDirCst:  func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextDir(a, &m.dir) },
	xfrmPcpuCst: func(m *xfrmModify, a *xfrmArgs) error { return xfrmNextU32(a, &m.pcpu) },
}

// xfrmStateModify is xfrm_state_modify: the loop, the checks after it, and
// the NEWSA or UPDSA with NLM_F_ACK to GOIP_WRITE_OUT.
func xfrmStateModify(c *runCtx, msgType uint16, args []string) error {
	m := &xfrmModify{c: c, pcpu: xfrmPcpuUnsetCst,
		req: xfrmAddattr{c: c, nlLen: xtcpnl.NlMsgHdrSizeCst + xtcpnl.XfrmUsersaInfoSizeCst, maxLen: xfrmRtaBufCst}}
	m.sa.family = uint16(c.family)
	for i := range 4 {
		m.sa.lft[i] = ^uint64(0)
	}
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		var err error
		if w, ok := xfrmModifyWords[a.cur()]; ok {
			err = w(m, a)
		} else {
			err = m.algoOrID(a)
		}
		if err != nil {
			return err
		}
	}
	if err := m.replayChecks(); err != nil {
		return err
	}
	if err := m.tailAttrs(); err != nil {
		return err
	}
	if m.sa.family == unix.AF_UNSPEC {
		m.sa.family = unix.AF_INET
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(msgType, unix.NLM_F_ACK, c.nextSeq(), m.sa.bytes(), m.req.attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// algoOrID is the loop's last branch: an algorithm type, or the ID, once.
func (m *xfrmModify) algoOrID(a *xfrmArgs) error {
	if typ, ok := xfrmAlgoTypes[a.cur()]; ok {
		return m.algo(a, typ)
	}
	if m.id {
		return xfrmUsage(invargErr("unknown", a.cur()))
	}
	m.id = true
	var f xfrmFilter
	if err := xfrmIDParse(m.c, a, &m.sa.id, &m.sa.family, &f, false); err != nil {
		return err
	}
	if m.c.family == unix.AF_UNSPEC {
		m.c.family = uint8(m.sa.family)
	}
	return nil
}

// replayChecks are the two replay window refusals after the loop.
func (m *xfrmModify) replayChecks() error {
	if m.sa.sflags&xfrmStateESNCst != 0 && m.window == 0 && m.dir != xfrmSADirOutCst {
		return fmt.Errorf("%w: Error: esn flag set without replay-window", ErrXfrmUsage)
	}
	if m.window > xfrmReplayESNMaxCst {
		return fmt.Errorf("%w: Error: replay-window (%d) > XFRMA_REPLAY_ESN_MAX (%d)", ErrXfrmUsage, m.window,
			xfrmReplayESNMaxCst)
	}
	return nil
}

// tailAttrs are the attributes and checks after the loop, in its order:
// the offload, the replay state, the extra flags, the ID's check, the mark,
// if_id, tfcpad, the protocol's checks, the output mark, the direction and
// the CPU.
func (m *xfrmModify) tailAttrs() error {
	r := &m.req
	var errs []error
	if m.offload {
		flags := uint8(0)
		if m.dir == xfrmSADirInCst {
			flags = xfrmOffloadInCst
		}
		if m.packet {
			flags |= xfrmOffloadPktCst
		}
		xuo := binary.LittleEndian.AppendUint32(nil, uint32(m.ifindex))
		_, err := r.add(xtcpnl.XfrmAttrOffloadDev, append(xuo, flags, 0, 0, 0))
		errs = append(errs, err)
	}
	errs = append(errs, m.replayAttr())
	if m.extraFlags != 0 {
		errs = append(errs, r.add32(xtcpnl.XfrmAttrSAExtraFlags, m.extraFlags))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if !m.id {
		return fmt.Errorf("%w: Not enough information: ID is required", ErrXfrmRefused)
	}
	if m.mark[1] != 0 {
		if err := xfrmMarkAttr(r, m.mark); err != nil {
			return err
		}
	}
	if m.ifIDSet {
		errs = append(errs, r.add32(xtcpnl.XfrmAttrIfID, m.ifID))
	}
	if m.tfcpad != 0 {
		errs = append(errs, r.add32(xtcpnl.XfrmAttrTFCPad, m.tfcpad))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := m.protoChecks(); err != nil {
		return err
	}
	return m.lastAttrs()
}

// replayAttr is the replay state: XFRMA_REPLAY_ESN_VAL with ESN or a
// window wider than the bitmap, else XFRMA_REPLAY_VAL when a sequence
// number was given, and the window, cut to its u8, in the SA.
func (m *xfrmModify) replayAttr() error {
	if m.sa.sflags&xfrmStateESNCst != 0 || m.window > xfrmReplayBitmapBitsCst {
		var b []byte
		for _, v := range []uint32{(m.window + 31) / 32, m.roseq, m.rseq, m.roseqHi, m.rseqHi, m.window} {
			b = binary.LittleEndian.AppendUint32(b, v)
		}
		_, err := m.req.add(xtcpnl.XfrmAttrReplayESNVal, b)
		return err
	}
	m.sa.window = uint8(m.window)
	if m.rseq == 0 && m.roseq == 0 {
		return nil
	}
	b := binary.LittleEndian.AppendUint32(nil, m.roseq)
	b = binary.LittleEndian.AppendUint32(b, m.rseq)
	_, err := m.req.add(xtcpnl.XfrmAttrReplayVal, append(b, 0, 0, 0, 0))
	return err
}

// lastAttrs are the output mark and its mask, the direction and the CPU.
func (m *xfrmModify) lastAttrs() error {
	r := &m.req
	var errs []error
	if m.outputMark[0] != 0 {
		errs = append(errs, r.add32(xtcpnl.XfrmAttrSetMark, m.outputMark[0]))
	}
	if m.outputMark[1] != 0 {
		errs = append(errs, r.add32(xtcpnl.XfrmAttrSetMarkMask, m.outputMark[1]))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := xfrmDirAttr(r, m.dir); err != nil {
		return err
	}
	if m.pcpu != xfrmPcpuUnsetCst {
		return r.add32(xtcpnl.XfrmAttrSAPcpu, m.pcpu)
	}
	return nil
}

// xfrmMarkAttr is XFRMA_MARK, and the exit when it does not fit.
func xfrmMarkAttr(r *xfrmAddattr, mark [2]uint32) error {
	b := binary.LittleEndian.AppendUint32(nil, mark[0])
	ok, err := r.add(xtcpnl.XfrmAttrMark, binary.LittleEndian.AppendUint32(b, mark[1]))
	if err == nil && !ok {
		err = fmt.Errorf("%w: XFRMA_MARK failed", ErrXfrmRefused)
	}
	return err
}

// xfrmDirAttr is XFRMA_SA_DIR when a direction was given, and the exit
// when it does not fit.
func xfrmDirAttr(r *xfrmAddattr, dir uint8) error {
	if dir == 0 {
		return nil
	}
	ok, err := r.add(xtcpnl.XfrmAttrSADir, []byte{dir})
	if err == nil && !ok {
		err = fmt.Errorf("%w: XFRMA_SA_DIR failed", ErrXfrmRefused)
	}
	return err
}

// protoChecks are the checks of the mode, the algorithms and coa against
// the protocol, each its own exit(1).
func (m *xfrmModify) protoChecks() error {
	proto := m.sa.id.proto
	name := render.XfrmProtoName(proto)
	refuse := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrXfrmRefused, fmt.Sprintf(format, args...))
	}
	switch proto {
	case unix.IPPROTO_ESP, unix.IPPROTO_AH, unix.IPPROTO_COMP:
		if m.sa.mode > 1 && (m.sa.mode != xfrmModeBeetCst || proto != unix.IPPROTO_ESP) {
			return refuse("MODE value is invalid with XFRM-PROTO value %q", name)
		}
		if err := m.algoChecks(proto, name); err != nil {
			return err
		}
	default:
		if m.enc || m.auth || m.aead || m.comp {
			return refuse("ALGO is invalid with XFRM-PROTO value %q", name)
		}
	}
	if proto == unix.IPPROTO_ROUTING || proto == unix.IPPROTO_DSTOPTS {
		switch m.sa.mode {
		case xfrmModeROCst, xfrmModeTrigCst:
		case 0:
			return refuse("\"mode\" is required with XFRM-PROTO value %q", name)
		default:
			return refuse("MODE value is invalid with XFRM-PROTO value %q", name)
		}
		if !m.coa {
			return refuse("\"coa\" is required with XFRM-PROTO value %q", name)
		}
	} else if m.coa {
		return refuse("\"coa\" is invalid with XFRM-PROTO value %q", name)
	}
	return nil
}

// algoChecks are the algorithm types each IPsec protocol needs and refuses.
func (m *xfrmModify) algoChecks(proto uint8, name string) error {
	var bad, missing string
	switch proto {
	case unix.IPPROTO_ESP:
		if m.comp {
			bad = "ALGO-TYPE value \"comp\" is invalid"
		} else if !m.enc && !m.aead {
			missing = "ALGO-TYPE value \"enc\" or \"aead\" is required"
		}
	case unix.IPPROTO_AH:
		if m.enc || m.aead || m.comp {
			bad = "ALGO-TYPE values \"enc\", \"aead\", and \"comp\" are invalid"
		} else if !m.auth {
			missing = "ALGO-TYPE value \"auth\" or \"auth-trunc\" is required"
		}
	default:
		if m.enc || m.auth || m.aead {
			bad = "ALGO-TYPE values \"enc\", \"auth\", \"auth-trunc\", and \"aead\" are invalid"
		} else if !m.comp {
			missing = "ALGO-TYPE value \"comp\" is required"
		}
	}
	if bad == "" && missing == "" {
		return nil
	}
	return fmt.Errorf("%w: %s%s with XFRM-PROTO value %q", ErrXfrmRefused, bad, missing, name)
}

// xfrmAlgoTypes is xfrm_algotype_getbyname's table.
var xfrmAlgoTypes = map[string]uint16{
	"enc": xtcpnl.XfrmAttrAlgCrypt, "auth": xtcpnl.XfrmAttrAlgAuth, "comp": xtcpnl.XfrmAttrAlgComp,
	"aead": xtcpnl.XfrmAttrAlgAEAD, "auth-trunc": xtcpnl.XfrmAttrAlgAuthTrunc,
}

// algo is an ALGO: the type, refused when its kind was given, then the
// name, the key but for comp, and aead's ICV length or auth-trunc's
// truncation length, as struct xfrm_algo, xfrm_algo_aead or xfrm_algo_auth
// with the key after it.
func (m *xfrmModify) algo(a *xfrmArgs, typ uint16) error {
	if err := m.algoKind(a.cur(), typ); err != nil {
		return err
	}
	name, err := xfrmNextOK(a, "ALGO-NAME")
	if err != nil {
		return err
	}
	key := ""
	if typ != xtcpnl.XfrmAttrAlgComp {
		if key, err = xfrmNextOK(a, "ALGO-KEYMAT"); err != nil {
			return err
		}
	}
	base, extra := xtcpnl.XfrmAlgoSizeCst, uint32(0)
	if typ == xtcpnl.XfrmAttrAlgAEAD || typ == xtcpnl.XfrmAttrAlgAuthTrunc {
		what := "ALGO-ICV-LEN"
		if typ == xtcpnl.XfrmAttrAlgAuthTrunc {
			what = "ALGO-TRUNC-LEN"
		}
		v, err := xfrmNextOK(a, what)
		if err != nil {
			return err
		}
		n, ok := getU32(v, 0)
		if !ok {
			return xfrmUsage(invargErr(what+" value is invalid", v))
		}
		base, extra = xtcpnl.XfrmAlgoICVSizeCst, n
	}
	k, err := xfrmAlgoKey(key)
	if err != nil {
		return err
	}
	b := make([]byte, base+len(k))
	copy(b[:xfrmAlgoNameLenCst-1], name)
	binary.LittleEndian.PutUint32(b[64:], uint32(len(k)*8))
	if base == xtcpnl.XfrmAlgoICVSizeCst {
		binary.LittleEndian.PutUint32(b[68:], extra)
	}
	copy(b[base:], k)
	_, err = m.req.add(typ, b)
	return err
}

// algoKind is the duplicate check of the algorithm's kind.
func (m *xfrmModify) algoKind(word string, typ uint16) error {
	var dup bool
	switch typ {
	case xtcpnl.XfrmAttrAlgAEAD:
		dup, m.aead = m.enc || m.auth || m.aead, true
	case xtcpnl.XfrmAttrAlgCrypt:
		dup, m.enc = m.enc || m.aead, true
	case xtcpnl.XfrmAttrAlgComp:
		dup, m.comp = m.comp, true
	default:
		dup, m.auth = m.auth || m.aead, true
	}
	if dup {
		return xfrmUsage(dupargErr("ALGO-TYPE", word))
	}
	return nil
}

// xfrmNextOK is NEXT_ARG_OK's missarg, then NEXT_ARG: the next word.
func xfrmNextOK(a *xfrmArgs, what string) (string, error) {
	if !a.nextOK() {
		return "", xfrmUsage(fmt.Errorf("Error: argument %q is required", what))
	}
	a.i++
	return a.cur(), nil
}

// xfrmAlgoKey is xfrm_algo_parse's key: after 0x, hex pairs, an odd count
// with a 0 ahead; else the word's bytes. Either is refused past 512 bytes.
func xfrmAlgoKey(key string) ([]byte, error) {
	overflow := xfrmUsage(invargErr("ALGO-KEYMAT value makes buffer overflow\n", key))
	if len(key) <= 2 || !strings.HasPrefix(key, "0x") {
		if len(key) > xfrmAlgoKeyBufCst {
			return nil, overflow
		}
		return []byte(key), nil
	}
	p := key[2:]
	n := (len(p) + 1) / 2
	if n > xfrmAlgoKeyBufCst {
		return nil, overflow
	}
	if len(p)%2 == 1 {
		p = "0" + p
	}
	out := make([]byte, n)
	for j := range n {
		v, ok := getU8(p[2*j:2*j+2], 16)
		if !ok {
			return nil, xfrmUsage(invargErr("ALGO-KEYMAT value is invalid", key))
		}
		out[j] = v
	}
	return out, nil
}

// selector is sel: preferred_family cleared for the parse, then the
// selector's.
func (m *xfrmModify) selector(a *xfrmArgs) error {
	if err := a.next(); err != nil {
		return err
	}
	m.c.family = unix.AF_UNSPEC
	if err := xfrmSelectorParse(m.c, a, &m.sa.sel); err != nil {
		return err
	}
	m.c.family = uint8(m.sa.sel.family)
	return nil
}

// xfrmEncapTypes is xfrm_encap_type_parse's words: UDP_ENCAP_ESPINUDP_NON_IKE,
// UDP_ENCAP_ESPINUDP and TCP_ENCAP_ESPINTCP.
var xfrmEncapTypes = map[string]uint16{"espinudp-nonike": 1, "espinudp": 2, "espintcp": 7}

// encap is ENCAP: the type, the two ports, get_be16 base 0, and the
// original address, get_addr's in any family, as struct xfrm_encap_tmpl.
func (m *xfrmModify) encap(a *xfrmArgs) error {
	if err := a.next(); err != nil {
		return err
	}
	typ, ok := xfrmEncapTypes[a.cur()]
	if !ok {
		return xfrmUsage(invargErr("ENCAP-TYPE value is invalid", a.cur()))
	}
	b := binary.LittleEndian.AppendUint16(nil, typ)
	for _, what := range []string{"SPORT", "DPORT"} {
		if err := a.next(); err != nil {
			return err
		}
		port, ok := getU16(a.cur(), 0)
		if !ok {
			return xfrmUsage(invargErr(what+" value after \"encap\" is invalid", a.cur()))
		}
		b = binary.BigEndian.AppendUint16(b, port)
	}
	if err := a.next(); err != nil {
		return err
	}
	oa, err := routeGetAddr(a.cur(), unix.AF_UNSPEC)
	if err != nil {
		return err
	}
	b = append(b, 0, 0)
	b = append(b, make([]byte, xtcpnl.XfrmAddressSizeCst)...)
	copy(b[8:], oa.data)
	_, err = m.req.add(xtcpnl.XfrmAttrEncap, b)
	return err
}

// careOf is coa, once: get_prefix's address in preferred_family, as
// XFRMA_COADDR.
func (m *xfrmModify) careOf(a *xfrmArgs) error {
	if m.coa {
		return xfrmUsage(dupargErr("coa", a.cur()))
	}
	m.coa = true
	if err := a.next(); err != nil {
		return err
	}
	p, err := routeGetPrefix(a.cur(), m.c.family)
	if err != nil {
		return err
	}
	if p.family == unix.AF_UNSPEC {
		return xfrmUsage(invargErr("value after \"coa\" has an unrecognized address family", a.cur()))
	}
	b := make([]byte, xtcpnl.XfrmAddressSizeCst)
	copy(b, p.data)
	_, err = m.req.add(xtcpnl.XfrmAttrCoAddr, b)
	return err
}

// secCtx is ctx, once: xfrm_sctx_parse's struct xfrm_user_sec_ctx, DOI and
// algorithm 1, and the context with its NUL. ip copies it into a 256-byte
// buffer without a check; goip refuses one that would overrun it.
func (m *xfrmModify) secCtx(a *xfrmArgs) error {
	if m.ctx {
		return xfrmUsage(dupargErr("ctx", a.cur()))
	}
	m.ctx = true
	if err := a.next(); err != nil {
		return err
	}
	s := a.cur()
	if len(s)+1 > xfrmCtxBufCst {
		return fmt.Errorf("a ctx of %d bytes, which overruns ip's %d-byte buffer: %w", len(s)+1, xfrmCtxBufCst,
			ErrNotImplemented)
	}
	b := xfrmSecCtxBytes(s)
	_, err := m.req.add(xtcpnl.XfrmAttrSecCtx, b)
	return err
}

// xfrmSecCtxBytes is xfrm_sctx_parse's struct and the context.
func xfrmSecCtxBytes(s string) []byte {
	slen := len(s) + 1
	b := binary.LittleEndian.AppendUint16(nil, uint16(xtcpnl.XfrmUserSecCtxSizeCst+slen))
	b = binary.LittleEndian.AppendUint16(b, xtcpnl.XfrmAttrSecCtx)
	b = append(b, 1, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(slen))
	b = append(b, s...)
	return append(b, 0)
}

// offloadDev is offload: crypto or packet, then dev and its index, then
// dir and its direction.
func (m *xfrmModify) offloadDev(a *xfrmArgs) error {
	if err := a.next(); err != nil {
		return err
	}
	switch a.cur() {
	case "crypto":
		if err := a.next(); err != nil {
			return err
		}
	case "packet":
		m.packet = true
		if err := a.next(); err != nil {
			return err
		}
	}
	if a.cur() != fouDevCst {
		return xfrmUsage(invargErr("Missing dev keyword", a.cur()))
	}
	if err := a.next(); err != nil {
		return err
	}
	idx, err := genlNameToIndex(m.c, a.cur())
	if err != nil {
		return err
	}
	if idx == 0 {
		return xfrmUsage(invargErr("Invalid device name", a.cur()))
	}
	m.ifindex = idx
	if err := a.next(); err != nil {
		return err
	}
	if a.cur() != xfrmDirCst {
		return xfrmUsage(invargErr("Missing DIR keyword", a.cur()))
	}
	if err := xfrmNextDir(a, &m.dir); err != nil {
		return err
	}
	m.offload = true
	return nil
}

// xfrmNextMode is mode's value, xfrm_mode_parse's.
func xfrmNextMode(a *xfrmArgs, mode *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	v, err := xfrmModeParse(a.cur())
	if err == nil {
		*mode = v
	}
	return err
}

// xfrmNextReqid is reqid's value, xfrm_reqid_parse's.
func xfrmNextReqid(a *xfrmArgs, reqid *uint32) error {
	if err := a.next(); err != nil {
		return err
	}
	v, err := xfrmReqidParse(a.cur())
	if err == nil {
		*reqid = v
	}
	return err
}

// xfrmNextSeq is seq's value, xfrm_seq_parse's get_be32 base 0.
func xfrmNextSeq(a *xfrmArgs, seq *uint32) error {
	if err := a.next(); err != nil {
		return err
	}
	v, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("SEQ value is invalid", a.cur()))
	}
	*seq = v
	return nil
}

// xfrmNextU32 is a word's get_u32 base 0 value, refused as `value after
// "WORD" is invalid`.
func xfrmNextU32(a *xfrmArgs, v *uint32) error {
	word := a.cur()
	if err := a.next(); err != nil {
		return err
	}
	n, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("value after \""+word+"\" is invalid", a.cur()))
	}
	*v = n
	return nil
}

// xfrmNextDir is dir's value, xfrm_dir_parse's: in or out.
func xfrmNextDir(a *xfrmArgs, dir *uint8) error {
	if err := a.next(); err != nil {
		return err
	}
	switch a.cur() {
	case "in":
		*dir = xfrmSADirInCst
	case xfrmDirOutCst:
		*dir = xfrmSADirOutCst
	default:
		return xfrmUsage(invargErr("DIR value is not \"in\" or \"out\"", a.cur()))
	}
	return nil
}

// xfrmExtraFlagParse is extra-flag's value, xfrm_state_extra_flag_parse's:
// a 0x value, get_u32 base 16, or a run of flag words, each ORed in, until
// another word, left as PREV_ARG leaves it.
func xfrmExtraFlagParse(a *xfrmArgs, flags *uint32) error {
	if err := a.next(); err != nil {
		return err
	}
	if v := a.cur(); len(v) > 2 && strings.HasPrefix(v, "0x") {
		n, ok := getU32(v, 16)
		if !ok {
			return xfrmUsage(invargErr("\"EXTRA-FLAG\" is invalid", v))
		}
		*flags = n
		return nil
	}
	for {
		switch a.cur() {
		case "dont-encap-dscp":
			*flags |= 1
		case "oseq-may-wrap":
			*flags |= 2
		default:
			a.i--
			return nil
		}
		if !a.nextOK() {
			return nil
		}
		a.i++
	}
}

// xfrmLifetimeWords are xfrm_lifetime_cfg_parse's words, each its u64's
// place in struct xfrm_lifetime_cfg.
var xfrmLifetimeWords = map[string]int{
	"byte-soft": 0, "byte-hard": 1, "packet-soft": 2, "packet-hard": 3,
	"time-soft": 4, "time-hard": 5, "time-use-soft": 6, "time-use-hard": 7,
}

// xfrmLifetimeParse is limit's LIMIT: a word and its get_u64 base 0 value.
func xfrmLifetimeParse(a *xfrmArgs, lft *[8]uint64) error {
	if err := a.next(); err != nil {
		return err
	}
	word := a.cur()
	i, ok := xfrmLifetimeWords[word]
	if !ok {
		return xfrmUsage(invargErr("LIMIT value is invalid", word))
	}
	if err := a.next(); err != nil {
		return err
	}
	v, ok := getU64(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("value after \""+word+"\" is invalid", a.cur()))
	}
	lft[i] = v
	return nil
}

// xfrmOutputMarkParse is output-mark's value, then mask and its value when
// the next word is mask.
func xfrmOutputMarkParse(a *xfrmArgs, mark *[2]uint32) error {
	if err := xfrmNextU32(a, &mark[0]); err != nil {
		return err
	}
	if !a.nextOK() {
		return nil
	}
	a.i++
	if a.cur() != xfrmMaskWordCst {
		a.i--
		return nil
	}
	if err := a.next(); err != nil {
		return err
	}
	n, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("mask value is invalid\n", a.cur()))
	}
	mark[1] = n
	return nil
}

// xfrmAllocspiMinCst and xfrmAllocspiMaxCst are allocspi's range when
// none is given, PF_KEY's; xfrmAllocspiCompMaxCst is IPComp's 16-bit max.
const (
	xfrmAllocspiMinCst     = 0x100
	xfrmAllocspiMaxCst     = 0x0fffffff
	xfrmAllocspiCompMaxCst = 0xffff
)

// xfrmSpiRange is allocspi's min and max, each with whether it was given.
type xfrmSpiRange struct {
	min, max       uint32
	minSet, maxSet bool
}

// word reads min or max, once each.
func (r *xfrmSpiRange) word(a *xfrmArgs) error {
	word := a.cur()
	v, set := &r.min, &r.minSet
	if word == "max" {
		v, set = &r.max, &r.maxSet
	}
	if *set {
		return xfrmUsage(dupargErr(word, a.cur()))
	}
	*set = true
	return xfrmNextU32(a, v)
}

// check is the range's checks after the loop, and its default.
func (r *xfrmSpiRange) check(proto uint8) error {
	switch {
	case r.minSet && !r.maxSet:
		return fmt.Errorf("%w: \"max\" is missing", ErrXfrmRefused)
	case r.minSet && r.min > r.max:
		return fmt.Errorf("%w: value after \"min\" is larger than value after \"max\"", ErrXfrmRefused)
	case r.maxSet && !r.minSet:
		return fmt.Errorf("%w: \"min\" is missing", ErrXfrmRefused)
	case !r.minSet:
		r.min, r.max = xfrmAllocspiMinCst, xfrmAllocspiMaxCst
		if proto == unix.IPPROTO_COMP {
			r.max = xfrmAllocspiCompMaxCst
		}
	}
	return nil
}

// xfrmStateAllocspi is xfrm_state_allocspi: mode, mark, reqid, seq, min,
// max, dir and pcpu-num by strcmp, else one ID, which may not name an SPI;
// then struct xfrm_userspi_info, its lifetimes zero, and the direction, the
// mark, when both its halves are set, and the CPU. ip sends it without
// NLM_F_ACK and prints the larval SA the kernel answers with; goip writes
// it to GOIP_WRITE_OUT and has no answer to print.
func xfrmStateAllocspi(c *runCtx, args []string) error {
	var sa xfrmSA
	sa.family = uint16(c.family)
	var rng xfrmSpiRange
	var mark [2]uint32
	var dir uint8
	pcpu := uint32(xfrmPcpuUnsetCst)
	id := false
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		var err error
		switch a.cur() {
		case xfrmModeCst:
			err = xfrmNextMode(a, &sa.mode)
		case xfrmMarkCst:
			err = xfrmParseMark(a, &mark)
		case xfrmReqidCst:
			err = xfrmNextReqid(a, &sa.reqid)
		case xfrmSeqCst:
			err = xfrmNextSeq(a, &sa.seq)
		case "min", "max":
			err = rng.word(a)
		case xfrmDirCst:
			err = xfrmNextDir(a, &dir)
		case xfrmPcpuCst:
			err = xfrmNextU32(a, &pcpu)
		default:
			err = xfrmAllocspiID(c, a, &sa, &id)
		}
		if err != nil {
			return err
		}
	}
	if !id {
		return fmt.Errorf("%w: Not enough information: ID is required", ErrXfrmRefused)
	}
	if err := rng.check(sa.id.proto); err != nil {
		return err
	}
	r := xfrmAddattr{c: c, nlLen: xtcpnl.NlMsgHdrSizeCst + xtcpnl.XfrmUserspiInfoSizeCst, maxLen: xfrmRtaBufCst}
	if err := xfrmDirAttr(&r, dir); err != nil {
		return err
	}
	if mark[0]&mark[1] != 0 {
		if err := xfrmMarkAttr(&r, mark); err != nil {
			return err
		}
	}
	if pcpu != xfrmPcpuUnsetCst {
		if err := r.add32(xtcpnl.XfrmAttrSAPcpu, pcpu); err != nil {
			return err
		}
	}
	if sa.family == unix.AF_UNSPEC {
		sa.family = unix.AF_INET
	}
	hdr := sa.bytes()
	hdr = binary.LittleEndian.AppendUint32(hdr, rng.min)
	hdr = binary.LittleEndian.AppendUint32(hdr, rng.max)
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgAllocSPI, 0, c.nextSeq(), hdr, r.attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmAllocspiID is allocspi's ID, once; an SPI in it is refused.
func xfrmAllocspiID(c *runCtx, a *xfrmArgs, sa *xfrmSA, id *bool) error {
	if *id {
		return xfrmUsage(invargErr("unknown", a.cur()))
	}
	*id = true
	var f xfrmFilter
	if err := xfrmIDParse(c, a, &sa.id, &sa.family, &f, false); err != nil {
		return err
	}
	if sa.id.spi != 0 {
		return fmt.Errorf("%w: \"spi\" is invalid", ErrXfrmRefused)
	}
	if c.family == unix.AF_UNSPEC {
		c.family = uint8(sa.family)
	}
	return nil
}

// xfrmStateFlush is xfrm_state_flush: proto, once, and its XFRM protocol
// by name, then one FLUSHSA, struct xfrm_usersa_flush, with NLM_F_ACK, to
// GOIP_WRITE_OUT.
func xfrmStateFlush(c *runCtx, args []string) error {
	proto, protoSet := uint8(0), false
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		if a.cur() != xfrmProtoCst {
			return xfrmUsage(invargErr("unknown", a.cur()))
		}
		if protoSet {
			return xfrmUsage(dupargErr(xfrmProtoCst, a.cur()))
		}
		protoSet = true
		if err := a.next(); err != nil {
			return err
		}
		p := render.XfrmProtoByName(a.cur())
		if p < 0 {
			return xfrmUsage(invargErr("XFRM-PROTO value is invalid", a.cur()))
		}
		proto = uint8(p)
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgFlushSA, unix.NLM_F_ACK, c.nextSeq(), []byte{proto}, nil)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmDeleteall is deleteall's state across its rounds: the DELSAs copied,
// each message's body, without its sequence number, to skip an SA copied
// in an earlier round, and the copies' bytes.
type xfrmDeleteall struct {
	c      *runCtx
	f      *xfrmFilter
	copied map[string]bool
	out    []byte
}

// xfrmStateDeleteall is xfrm_state_list_or_deleteall's deleteall: the
// list's words, then rounds of a GETSA dump with no filter attribute, each
// answer xfrm_state_keep copies a DELSA of, until a round copies none. ip
// sends each round's copies in one datagram; goip writes every round's to
// GOIP_WRITE_OUT, when there are any.
func xfrmStateDeleteall(c *runCtx, args []string) error {
	f, _, err := xfrmStateListArgs(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	d := &xfrmDeleteall{c: c, f: &f, copied: map[string]bool{}}
	for {
		req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetSA, unix.NLM_F_DUMP, c.nextSeq(), nil, nil)
		if err != nil {
			return err
		}
		msgs, err := xs.XfrmDump(req)
		n := 0
		if err == nil {
			n, err = d.round(msgs)
		}
		if errors.Is(err, ErrNotImplemented) {
			return err
		}
		if err != nil {
			return fmt.Errorf("%w: Delete-all terminated: %w", ErrXfrmDumpTerminated, err)
		}
		if n == 0 {
			break
		}
	}
	if len(d.out) == 0 {
		return nil
	}
	return writeAddrOut(d.out)
}

// round is one dump's answers through xfrm_state_keep: the copies' count.
func (d *xfrmDeleteall) round(msgs []xfrmMsg) (int, error) {
	var batch []byte
	n := 0
	for _, m := range msgs {
		del, err := d.keep(m, len(batch))
		if err != nil {
			return 0, err
		}
		if del == nil {
			continue
		}
		batch = append(batch, del...)
		n++
	}
	d.out = append(d.out, batch...)
	return n, nil
}

// keep is xfrm_state_keep: another type than NEWSA is "Not a state" on
// stderr; a body short of its struct ends the dump; an SA the filter does
// not match, an IPIP or IPv6 one, or one an earlier round copied, is
// skipped; past the buffer is "State buffer overflow"; else a DELSA of its
// ID, source and mark, without NLM_F_ACK.
func (d *xfrmDeleteall) keep(m xfrmMsg, offset int) ([]byte, error) {
	if m.typ != xtcpnl.XfrmMsgNewSA {
		_, err := fmt.Fprintf(d.c.errOut, "Not a state: %08x %08x %08x\n", xtcpnl.NlMsgHdrSizeCst+len(m.body), m.typ,
			m.flags)
		return nil, err
	}
	if len(m.body) < xtcpnl.XfrmUsersaInfoSizeCst {
		return nil, fmt.Errorf("BUG: wrong nlmsg len %d", len(m.body)-xtcpnl.XfrmUsersaInfoSizeCst)
	}
	s, err := xtcpnl.ParseXfrmState(xtcpnl.NlMsgHdr{Type: m.typ, Flags: m.flags}, m.body)
	if err != nil {
		return nil, err
	}
	if !d.f.match(s.Info) || m.body[76] == unix.IPPROTO_IPIP || m.body[76] == unix.IPPROTO_IPV6 {
		return nil, nil
	}
	if offset > xfrmDeleteallBufCst {
		return nil, fmt.Errorf("%w: State buffer overflow", ErrXfrmRefused)
	}
	hdr := make([]byte, xtcpnl.XfrmUsersaIDSizeCst)
	copy(hdr, m.body[56:76])
	copy(hdr[20:22], m.body[212:214])
	hdr[22] = m.body[76]
	r := xfrmAddattr{c: d.c, nlLen: xtcpnl.NlMsgHdrSizeCst + xtcpnl.XfrmUsersaIDSizeCst, maxLen: xfrmDeleteallBufCst}
	if _, err := r.add(xtcpnl.XfrmAttrSrcAddr, m.body[80:96]); err != nil {
		return nil, err
	}
	mark, err := xfrmDeleteallMark(m.body, xtcpnl.XfrmUsersaInfoSizeCst)
	if err != nil {
		return nil, err
	}
	if mark != nil {
		ok, err := r.add(xtcpnl.XfrmAttrMark, mark)
		if err == nil && !ok {
			err = fmt.Errorf("%w: xfrm_state_keep: XFRMA_MARK failed", ErrXfrmRefused)
		}
		if err != nil {
			return nil, err
		}
	}
	del, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgDelSA, 0, 0, hdr, r.attrs)
	if err != nil {
		return nil, err
	}
	key := string(del[xtcpnl.NlMsgHdrSizeCst:])
	if d.copied[key] {
		return nil, nil
	}
	d.copied[key] = true
	if offset+len(del) > xfrmDeleteallBufCst {
		return nil, fmt.Errorf("a deleteall round of more than %d bytes, which overruns ip's buffer: %w",
			xfrmDeleteallBufCst, ErrNotImplemented)
	}
	binary.LittleEndian.PutUint32(del[8:], d.c.nextSeq())
	return del, nil
}

// xfrmDeleteallMark is what xfrm_state_keep copies of the SA's last
// XFRMA_MARK, and xfrm_policy_keep of the policy's: rta_len bytes from its
// data, the mark and the four bytes after it, the attributes starting
// after the message's struct, hdrLen bytes. goip refuses a mark whose copy
// would read past the message, where ip reads what its buffer holds there.
func xfrmDeleteallMark(body []byte, hdrLen int) ([]byte, error) {
	var mark []byte
	for off := hdrLen; off+4 <= len(body); {
		l := int(binary.LittleEndian.Uint16(body[off:]))
		if l < 4 || off+l > len(body) {
			break
		}
		if binary.LittleEndian.Uint16(body[off+2:]) == xtcpnl.XfrmAttrMark {
			if off+4+l > len(body) {
				return nil, fmt.Errorf("a message whose XFRMA_MARK ends it, which ip copies %d bytes past: %w",
					l-xfrmMarkLenCst, ErrNotImplemented)
			}
			mark = body[off+4 : off+4+l]
		}
		off += nlmsgAlign(l)
	}
	return mark, nil
}
