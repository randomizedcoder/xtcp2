package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	macsecOnCst  = "on"
	macsecOffCst = "off"
	macsecSACst  = "sa"
	macsecXpnCst = "xpn"
	// macsecUnsetCst is the 0xff an SA's AN and active, and a channel's
	// active, start as: not given.
	macsecUnsetCst = 0xff
	// macsecBufLenCst is MACSEC_BUFLEN, the room ipmacsec.c gives a
	// request.
	macsecBufLenCst = 1024
	// The lengths ipmacsec.c reads a key id, a key and a salt into,
	// MACSEC_KEYID_LEN, MACSEC_MAX_KEY_LEN and MACSEC_SALT_LEN of its
	// copy of the uapi header.
	macsecKeyIDLenCst = 16
	macsecMaxKeyCst   = 128
	macsecSaltLenCst  = 12
	// macsecAddrLenCst is struct sci's abuf, the lladdr's room.
	macsecAddrLenCst = 6
)

// macsecInvarg marks err, invarg's, duparg2's or incomplete_command's, as
// one of ErrMacsecUsage's refusals.
func macsecInvarg(err error) error {
	return fmt.Errorf("%w: %w", ErrMacsecUsage, err)
}

// macsecUnknown is parse_sa_args' and parse_rxsci_args' word they do not
// know: its message, then usage.
func macsecUnknown(arg string) error {
	return fmt.Errorf("%w: macsec: unknown command %q?\n%s", ErrMacsecUsage, arg, macsecUsageCst)
}

// macsecSA is struct sa_desc as parse_sa_args fills it. pn is the union:
// pn's u32 sets its low half, xpn's u64 all of it. The key id and key
// buffers keep what an earlier key wrote past a later one's bytes.
type macsecSA struct {
	an, active       uint8
	pn               uint64
	xpn              bool
	keyID            [macsecKeyIDLenCst]byte
	key              [macsecMaxKeyCst]byte
	keyLen           int
	salt             [macsecSaltLenCst]byte
	ssci             uint32
	saltSet, ssciSet bool
}

// parseArgs is parse_sa_args: each word by strcmp, every argument used.
func (sa *macsecSA) parseArgs(args []string) error {
	activeSet := false
	for i := 0; i < len(args); i++ {
		var err error
		switch arg := args[i]; arg {
		case "pn", macsecXpnCst:
			i, err = sa.parsePN(args, i)
		case "salt":
			i, err = sa.parseSalt(args, i)
		case "ssci":
			i, err = sa.parseSSCI(args, i)
		case "key":
			i, err = sa.parseKey(args, i)
		case macsecOnCst, macsecOffCst:
			if activeSet {
				return macsecInvarg(duparg2Err("on/off", arg))
			}
			sa.active, activeSet = 0, true
			if arg == macsecOnCst {
				sa.active = 1
			}
		default:
			return macsecUnknown(arg)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// parsePN is pn's value, get_u32 base 0, or xpn's, get_u64 base 0, which
// also marks the SA XPN: either refused when a PN is set, or for zero.
func (sa *macsecSA) parsePN(args []string, i int) (int, error) {
	word := args[i]
	if sa.pn != 0 {
		return i, macsecInvarg(duparg2Err(word, word))
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	var v uint64
	var ok bool
	if word == macsecXpnCst {
		v, ok = getU64(args[i], 0)
	} else {
		var v32 uint32
		v32, ok = getU32(args[i], 0)
		v = uint64(v32)
	}
	switch {
	case !ok:
		return i, macsecInvarg(invargErr("expected pn", args[i]))
	case v == 0:
		return i, macsecInvarg(invargErr("expected pn != 0", args[i]))
	}
	sa.pn = v
	sa.xpn = sa.xpn || word == macsecXpnCst
	return i, nil
}

// parseSalt is salt's value, hexstring_a2n into the 12 bytes.
func (sa *macsecSA) parseSalt(args []string, i int) (int, error) {
	if sa.saltSet {
		return i, macsecInvarg(duparg2Err("salt", "salt"))
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	if _, ok := macsecHex(args[i], sa.salt[:]); !ok {
		return i, macsecInvarg(invargErr("expected salt", args[i]))
	}
	sa.saltSet = true
	return i, nil
}

// parseSSCI is ssci's value, get_be32 base 16.
func (sa *macsecSA) parseSSCI(args []string, i int) (int, error) {
	if sa.ssciSet {
		return i, macsecInvarg(duparg2Err("ssci", "ssci"))
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	v, ok := getU32(args[i], 16)
	if !ok {
		return i, macsecInvarg(invargErr("expected ssci", args[i]))
	}
	sa.ssci, sa.ssciSet = v, true
	return i, nil
}

// parseKey is key's two values, the key id and the key, each
// hexstring_a2n's; a second key is no duplicate.
func (sa *macsecSA) parseKey(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	if _, ok := macsecHex(args[i], sa.keyID[:]); !ok {
		return i, macsecInvarg(invargErr("expected key id", args[i]))
	}
	if i, err = nextArg(args, i); err != nil {
		return i, macsecInvarg(err)
	}
	n, ok := macsecHex(args[i], sa.key[:])
	if !ok {
		return i, macsecInvarg(invargErr("expected key", args[i]))
	}
	sa.keyLen = n
	return i, nil
}

// check is check_sa_args: an add needs a key and a PN, a set must not
// change the key.
func (sa *macsecSA) check(v *macsecVerb) error {
	switch {
	case v.add && sa.keyLen == 0:
		return fmt.Errorf("%w: cannot create SA without key", ErrMacsecRefused)
	case v.add && sa.pn == 0:
		return fmt.Errorf("%w: must specify a packet number != 0", ErrMacsecRefused)
	case v.upd && sa.keyLen != 0:
		return fmt.Errorf("%w: cannot change key on SA", ErrMacsecRefused)
	}
	return nil
}

// macsecHex is hexstring_a2n into buf: an odd length is refused, then
// each pair of characters is strtoul base 16's, which must use both and
// fit a byte, up to len(buf) pairs, the rest unread. It returns the bytes
// written; the rest of buf keeps what it held.
func macsecHex(s string, buf []byte) (int, bool) {
	if len(s)%2 != 0 {
		return 0, false
	}
	n := 0
	for ; n < len(buf) && len(s) > 1; n++ {
		r := cStrtoul(s[:2], 16)
		if r.end != 2 || r.val > 0xff {
			return n, false
		}
		buf[n] = byte(r.val)
		s = s[2:]
	}
	return n, true
}

// macsecGetSA is get_sa: false when the next word is not sa, else the AN,
// get_u8 base 0 and at most 3.
func macsecGetSA(args []string) (uint8, []string, bool, error) {
	if len(args) == 0 || args[0] != macsecSACst {
		return 0, args, false, nil
	}
	i, err := nextArg(args, 0)
	if err != nil {
		return 0, args, false, macsecInvarg(err)
	}
	an, ok := getU8(args[i], 0)
	if !ok || an > 3 {
		return 0, args, false, macsecInvarg(invargErr("expected an { 0..3 }", args[i]))
	}
	return an, args[i+1:], true, nil
}

// macsecRxSci is parse_rxsci's get_sci_portaddr, not optional and not
// port alone: sci, get_be64 base 16, or port, get_be16 base 0 and not
// zero, and address, ll_addr_a2n into six bytes, read until one of the
// two is complete, which make_sci joins as the address then the port. It
// returns the SCI as its bytes go out, and the words after it.
func macsecRxSci(args []string) ([8]byte, []string, error) {
	var sci [8]byte
	var addr [macsecAddrLenCst]byte
	var port uint16
	var s, p, a bool
	i := 0
	for ; i < len(args) && !macsecSciComplete(s, p, a); i++ {
		var err error
		switch args[i] {
		case "sci":
			switch {
			case p:
				return sci, nil, macsecInvarg(invargErr("expected address", args[i]))
			case a:
				return sci, nil, macsecInvarg(invargErr("expected port", args[i]))
			}
			i, err = macsecSciValue(args, i, &sci)
			s = true
		case "port":
			i, err = macsecPortValue(args, i, &port)
			p = true
		case tcpmAddressCst:
			i, err = macsecAddrValue(args, i, &addr)
			a = true
		default:
			return sci, nil, macsecInvarg(invargErr("expected sci, port, or address", args[i]))
		}
		if err != nil {
			return sci, nil, err
		}
	}
	if !macsecSciComplete(s, p, a) {
		return sci, nil, fmt.Errorf("%w: expected sci\n%s", ErrMacsecUsage, macsecUsageCst)
	}
	if p && a {
		copy(sci[:], addr[:])
		binary.BigEndian.PutUint16(sci[macsecAddrLenCst:], port)
	}
	return sci, args[i:], nil
}

// macsecSciComplete is sci_complete, not port alone: an SCI, or a port and
// an address.
func macsecSciComplete(s, p, a bool) bool {
	return s || (p && a)
}

// macsecSciValue is sci's value, get_be64 base 16.
func macsecSciValue(args []string, i int, sci *[8]byte) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	v, ok := getU64(args[i], 16)
	if !ok {
		return i, macsecInvarg(invargErr("expected sci", args[i]))
	}
	binary.BigEndian.PutUint64(sci[:], v)
	return i, nil
}

// macsecPortValue is port's value, get_be16 base 0, not zero.
func macsecPortValue(args []string, i int, port *uint16) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	v, ok := getU16(args[i], 0)
	switch {
	case !ok:
		return i, macsecInvarg(invargErr("expected port", args[i]))
	case v == 0:
		return i, macsecInvarg(invargErr("expected port != 0", args[i]))
	}
	*port = v
	return i, nil
}

// macsecAddrValue is address's value, ll_addr_a2n into the six bytes,
// which keep what an earlier address wrote past a shorter one. Past six
// colon-separated parts ll_addr_a2n stops reading, the rest unread.
func macsecAddrValue(args []string, i int, addr *[macsecAddrLenCst]byte) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, macsecInvarg(err)
	}
	v := args[i]
	if strings.IndexByte(v, '.') < 0 {
		if parts := strings.SplitN(v, ":", macsecAddrLenCst+1); len(parts) > macsecAddrLenCst {
			v = strings.Join(parts[:macsecAddrLenCst], ":")
		}
	}
	b, ok := llAddrA2N(v, macsecAddrLenCst)
	if !ok {
		return i, macsecInvarg(invargErr("expected lladdr", args[i]))
	}
	copy(addr[:], b)
	return i, nil
}

// macsecModify is do_modify: usage with no device, the device, usage with
// nothing after it, then tx's SA or rx's channel, and the one request.
func macsecModify(c *runCtx, id uint16, v *macsecVerb, args []string) error {
	if len(args) == 0 {
		return macsecUsage()
	}
	ifindex, err := macsecDev(c, args[0])
	if err != nil {
		return err
	}
	args = args[1:]
	if len(args) == 0 {
		return macsecUsage()
	}
	var cmd uint8
	var attrs []byte
	switch args[0] {
	case "tx":
		cmd, attrs, err = macsecTxsa(v, ifindex, args[1:])
	case "rx":
		cmd, attrs, err = macsecRxsc(v, ifindex, args[1:])
	default:
		return macsecUsage()
	}
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.MacsecGenlName, id, cmd, xtcpnl.MacsecGenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// macsecTxsa is do_modify_txsa: sa and its AN, usage without, then for an
// add or a set the SA's words and check_sa_args; a delete reads no more.
func macsecTxsa(v *macsecVerb, ifindex int32, args []string) (uint8, []byte, error) {
	sa := macsecSA{an: macsecUnsetCst, active: macsecUnsetCst}
	an, rest, ok, err := macsecGetSA(args)
	switch {
	case err != nil:
		return 0, nil, err
	case !ok:
		return 0, nil, macsecUsage()
	}
	sa.an = an
	if !v.del {
		if err := sa.parseArgs(rest); err != nil {
			return 0, nil, err
		}
		if err := sa.check(v); err != nil {
			return 0, nil, err
		}
	}
	attrs, err := macsecAttrs(v.del, ifindex, nil, &sa)
	return v.txsa, attrs, err
}

// macsecRxChannel is struct rxsc_desc's SCI and active.
type macsecRxChannel struct {
	sci    [8]byte
	active uint8
}

// macsecRxsc is do_modify_rxsci: the SCI, usage without, then sa and its
// AN when given; for an add or a set the SA's words and check_sa_args, or
// the channel's on or off. A delete reads no more.
func macsecRxsc(v *macsecVerb, ifindex int32, args []string) (uint8, []byte, error) {
	if len(args) == 0 {
		return 0, nil, fmt.Errorf("%w: expected sci\n%s", ErrMacsecUsage, macsecUsageCst)
	}
	sci, rest, err := macsecRxSci(args)
	if err != nil {
		return 0, nil, err
	}
	rx := macsecRxChannel{sci: sci, active: macsecUnsetCst}
	sa := macsecSA{an: macsecUnsetCst, active: macsecUnsetCst}
	an, rest, saSet, err := macsecGetSA(rest)
	if err != nil {
		return 0, nil, err
	}
	cmd := v.rxsc
	if saSet {
		sa.an, cmd = an, v.rxsa
	}
	switch {
	case v.del:
	case saSet:
		if err := sa.parseArgs(rest); err != nil {
			return 0, nil, err
		}
		if err := sa.check(v); err != nil {
			return 0, nil, err
		}
	default:
		if err := rx.parseArgs(rest); err != nil {
			return 0, nil, err
		}
	}
	attrs, err := macsecAttrs(v.del, ifindex, &rx, &sa)
	return cmd, attrs, err
}

// parseArgs is parse_rxsci_args: on or off, once.
func (rx *macsecRxChannel) parseArgs(args []string) error {
	activeSet := false
	for _, arg := range args {
		if arg != macsecOnCst && arg != macsecOffCst {
			return macsecUnknown(arg)
		}
		if activeSet {
			return macsecInvarg(duparg2Err("on/off", arg))
		}
		rx.active, activeSet = 0, true
		if arg == macsecOnCst {
			rx.active = 1
		}
	}
	return nil
}

// macsecAttrs is do_modify_nl's attributes: the ifindex, the channel's
// nest when there is one, its SCI and, but for a delete, its active when
// given, then the SA's nest when an AN was given: the AN and, but for a
// delete, an XPN SA's u64 PN, salt and SSCI or another's u32 PN, each when
// given, the key id and key when a key was, and active when given.
func macsecAttrs(del bool, ifindex int32, rx *macsecRxChannel, sa *macsecSA) ([]byte, error) {
	var raw [macsecBufLenCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	errs := []error{b.PutU32(xtcpnl.MacsecAttrIfindex, uint32(ifindex))}
	if rx != nil {
		m, err := b.BeginNest(xtcpnl.MacsecAttrRxscConfig)
		if err != nil {
			return nil, err
		}
		errs = append(errs, b.PutBytes(xtcpnl.MacsecRxscAttrSCI, rx.sci[:]))
		if !del && rx.active != macsecUnsetCst {
			errs = append(errs, b.PutU8(xtcpnl.MacsecRxscAttrActive, rx.active))
		}
		errs = append(errs, b.EndNest(m))
	}
	if sa.an != macsecUnsetCst {
		m, err := b.BeginNest(xtcpnl.MacsecAttrSAConfig)
		if err != nil {
			return nil, err
		}
		errs = append(errs, b.PutU8(xtcpnl.MacsecSAAttrAN, sa.an))
		if !del {
			errs = append(errs, sa.putConfig(&b)...)
		}
		errs = append(errs, b.EndNest(m))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// putConfig is an add's or a set's SA attributes after the AN. addattr64
// copies the host's u64, and the SSCI is already big-endian.
func (sa *macsecSA) putConfig(b *xtcpnl.AttrBuilder) []error {
	errs := make([]error, 0, 3)
	switch {
	case sa.xpn:
		if sa.pn != 0 {
			errs = append(errs, b.PutBytes(xtcpnl.MacsecSAAttrPN, binary.LittleEndian.AppendUint64(nil, sa.pn)))
		}
		if sa.saltSet {
			errs = append(errs, b.PutBytes(xtcpnl.MacsecSAAttrSalt, sa.salt[:]))
		}
		if sa.ssciSet {
			errs = append(errs, b.PutBytes(xtcpnl.MacsecSAAttrSSCI, binary.BigEndian.AppendUint32(nil, sa.ssci)))
		}
	case sa.pn != 0:
		errs = append(errs, b.PutU32(xtcpnl.MacsecSAAttrPN, uint32(sa.pn)))
	}
	if sa.keyLen != 0 {
		errs = append(errs, b.PutBytes(xtcpnl.MacsecSAAttrKeyID, sa.keyID[:]),
			b.PutBytes(xtcpnl.MacsecSAAttrKey, sa.key[:sa.keyLen]))
	}
	if sa.active != macsecUnsetCst {
		errs = append(errs, b.PutU8(xtcpnl.MacsecSAAttrActive, sa.active))
	}
	return errs
}

// macsecOffloadNames is offload_str, in enum macsec_offload's order.
var macsecOffloadNames = []string{macsecOffCst, "phy", "mac"}

// macsecOffload is do_offload: usage with no device, the device, usage
// with no type, the type parse_one_of reads, then UPD_OFFLOAD; any word
// after the type is unread.
func macsecOffload(c *runCtx, id uint16, args []string) error {
	if len(args) == 0 {
		return macsecUsage()
	}
	ifindex, err := macsecDev(c, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return macsecUsage()
	}
	typ, err := macsecOffloadType(c.errOut, args[1])
	if err != nil {
		return err
	}
	var raw [macsecBufLenCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	if err := b.PutU32(xtcpnl.MacsecAttrIfindex, uint32(ifindex)); err != nil {
		return err
	}
	m, err := b.BeginNest(xtcpnl.MacsecAttrOffload)
	if err != nil {
		return err
	}
	if err := errors.Join(b.PutU8(xtcpnl.MacsecOffloadAttrType, typ), b.EndNest(m)); err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.MacsecGenlName, id, xtcpnl.MacsecCmdUpdOffload,
		xtcpnl.MacsecGenlVersion, unix.NLM_F_ACK, c.nextSeq(), b.Bytes())
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// macsecOffloadType is parse_one_of("offload", ...) with matches_warn:
// the first name the value is a prefix of, with matches_warn's warning
// for a prefix shorter than the name; for none, its message, then usage.
func macsecOffloadType(errOut io.Writer, v string) (uint8, error) {
	for i, name := range macsecOffloadNames {
		if !matchesPrefix(v, name) {
			continue
		}
		if len(v) != len(name) {
			if _, err := fmt.Fprintf(errOut, "WARNING: '%s' matches '%s' by prefix.\n"+
				"Matching by prefix is deprecated in this context, please use the full string.\n", v, name); err != nil {
				return 0, err
			}
		}
		return uint8(i), nil
	}
	return 0, fmt.Errorf("%w: Error: argument of \"offload\" must be one of \"off\", \"phy\", \"mac\", not %q\n%s",
		ErrMacsecUsage, v, macsecUsageCst)
}
