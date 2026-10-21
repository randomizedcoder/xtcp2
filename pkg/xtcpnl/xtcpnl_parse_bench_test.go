package xtcpnl

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// P0: a table-driven parse benchmark for each existing parser, over
// captured records (docs/netlink/goip-parity-gaps.md, "P0 — Benchmarks for
// the existing parsers"). Each benchmark decodes the smallest, a typical
// and the largest body of its type the kernel sent in the committed corpus.

// corpusMsg is one message out of the committed corpus, with its pcap and,
// for a generic netlink message, the name of its family, resolved from the
// NEWFAMILY replies in the same pcap, since family ids are assigned at run
// time.
type corpusMsg struct {
	path   string
	family string
	m      capturedNlMsg
}

var (
	corpusOnce sync.Once
	corpusMsgs []corpusMsg
)

// corpusAll returns every message in the DLT 253 pcaps under testdata, in
// path order, read once per test binary. pcapng files and other link types
// are skipped.
func corpusAll(tb testing.TB) []corpusMsg {
	tb.Helper()
	corpusOnce.Do(func() {
		var paths []string
		err := filepath.WalkDir(tdBase, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".pcap") {
				paths = append(paths, path)
			}
			return err
		})
		if err != nil {
			tb.Fatal(err)
		}
		for _, path := range paths {
			bs, err := os.ReadFile(path)
			if err != nil {
				tb.Fatal(err)
			}
			if _, _, err := ParseNetlinkPcap(bs); errors.Is(err, ErrPcapNotNetlink) || errors.Is(err, ErrPcapBadMagic) {
				continue
			}
			msgs := pcapNlMsgs(tb, path)
			families := pcapGenlFamilies(tb, msgs)
			for _, m := range msgs {
				c := corpusMsg{path: path, m: m}
				if m.proto == unix.NETLINK_GENERIC {
					c.family = families[m.h.Type]
				}
				corpusMsgs = append(corpusMsgs, c)
			}
		}
	})
	if len(corpusMsgs) == 0 {
		tb.Fatal("the corpus holds no netlink messages")
	}
	return corpusMsgs
}

// benchRecord is one captured message a parse benchmark decodes: its row
// name, its pcap, its header and its body.
type benchRecord struct {
	name string
	path string
	h    NlMsgHdr
	body []byte
}

// benchKind is the messages a parser's picks come from: a protocol,
// either a message type or, for generic netlink, a family name and perhaps
// its commands or the kernel's answers or events alone, and whether they
// are ip's requests rather than the kernel's messages.
type benchKind struct {
	proto   uint16
	typ     uint16
	family  string
	cmds    []uint8
	origin  benchOrigin
	request bool
}

// benchOrigin narrows the kernel's messages by nlmsg_pid: an answer, a dump
// included, carries the requester's port id, and a multicast event 0.
type benchOrigin uint8

const (
	anyOrigin benchOrigin = iota
	answerOrigin
	eventOrigin
)

// rtnl is the rtnetlink messages of type typ.
func rtnl(typ uint16) benchKind { return benchKind{proto: unix.NETLINK_ROUTE, typ: typ} }

// rtnlRequest is ip's rtnetlink requests of type typ, for a type only ip
// sends.
func rtnlRequest(typ uint16) benchKind {
	return benchKind{proto: unix.NETLINK_ROUTE, typ: typ, request: true}
}

// genl is the generic netlink messages of the family called family.
func genl(family string) benchKind { return benchKind{proto: unix.NETLINK_GENERIC, family: family} }

// genlAnswers is the kernel's answers in the family called family, not its
// events, for a family whose events reuse its command numbers.
func genlAnswers(family string) benchKind {
	k := genl(family)
	k.origin = answerOrigin
	return k
}

// genlEvents is the kernel's multicast events in the family called family.
func genlEvents(family string) benchKind {
	k := genl(family)
	k.origin = eventOrigin
	return k
}

// genlCmds is the generic netlink messages of the family called family
// whose command is one of cmds, for a family whose commands need different
// parsers.
func genlCmds(family string, cmds ...uint8) benchKind {
	k := genl(family)
	k.cmds = cmds
	return k
}

// String is the kind as TestParseBenchCases pins it: proto/type or
// proto/family, then /cmd and the commands, /answers or /events, then
// /request for ip's requests.
func (k benchKind) String() string {
	s := strconv.Itoa(int(k.proto)) + "/" + strconv.Itoa(int(k.typ))
	if k.family != "" {
		s = strconv.Itoa(int(k.proto)) + "/" + k.family
	}
	if len(k.cmds) != 0 {
		cmds := make([]string, 0, len(k.cmds))
		for _, c := range k.cmds {
			cmds = append(cmds, strconv.Itoa(int(c)))
		}
		s += "/cmd" + strings.Join(cmds, ",")
	}
	switch k.origin {
	case anyOrigin:
	case answerOrigin:
		s += "/answers"
	case eventOrigin:
		s += "/events"
	}
	if k.request {
		s += "/request"
	}
	return s
}

// matches reports whether c is of kind k: of its protocol, sent by the
// kernel, or for a request kind by ip (NLM_F_REQUEST), of one of its
// commands if it names any, of its origin if it names one, and of its type
// or family.
func (k benchKind) matches(c corpusMsg) bool {
	if c.m.proto != k.proto || (c.m.h.Flags&unix.NLM_F_REQUEST != 0) != k.request {
		return false
	}
	if len(k.cmds) != 0 && (len(c.m.body) == 0 || !slices.Contains(k.cmds, c.m.body[0])) {
		return false
	}
	if k.origin != anyOrigin && (c.m.h.Pid == 0) != (k.origin == eventOrigin) {
		return false
	}
	if k.family != "" {
		return c.family == k.family
	}
	return c.m.h.Type == k.typ
}

// benchPick returns the smallest, a typical (the median length) and the
// largest body of kind k, or nil when msgs holds none. Unless k is a
// request kind, those are bodies the kernel sent, in messages without
// NLM_F_REQUEST. Equal lengths keep msgs' order, so the choice changes only
// when the corpus does.
func benchPick(msgs []corpusMsg, k benchKind) []benchRecord {
	var all []corpusMsg
	for _, c := range msgs {
		if k.matches(c) {
			all = append(all, c)
		}
	}
	if len(all) == 0 {
		return nil
	}
	slices.SortStableFunc(all, func(a, b corpusMsg) int { return len(a.m.body) - len(b.m.body) })
	pick := func(name string, c corpusMsg) benchRecord {
		return benchRecord{name: name, path: c.path, h: c.m.h, body: c.m.body}
	}
	return []benchRecord{
		pick("smallest", all[0]),
		pick("typical", all[len(all)/2]),
		pick("largest", all[len(all)-1]),
	}
}

// benchRecords is benchPick over the committed corpus. A kind the corpus
// has no kernel message of fails the benchmark.
func benchRecords(tb testing.TB, k benchKind) []benchRecord {
	tb.Helper()
	recs := benchPick(corpusAll(tb), k)
	if recs == nil {
		tb.Fatalf("no kernel message of kind %s in the corpus", k)
	}
	return recs
}

var (
	resultLink    LinkInfo
	resultRoute   RouteInfo
	resultNeigh   NeighInfo
	resultRule    RuleInfo
	resultNexthop NexthopInfo
	resultLabel   AddrLabelInfo
	resultNtbl    NeighTblInfo
	resultNetconf NetconfInfo
	resultStats   IfStatsInfo
	resultNlErr   NlMsgErr
	resultEvent   any
	resultGenl    GenericNetlinkMessage
	resultFamily  GenericNetlinkFamily
	resultEthtool EthtoolMessage
	resultNsid    NsidInfo
	resultBucket  NexthopBucketInfo
	resultProp    LinkPropInfo
	resultPrefix  PrefixInfo
	resultTCPM    TCPMetrics
	resultSeg6    Seg6
	resultIOAM6   IOAM6
	resultIOAM6Ev IOAM6Event
	resultMPTCP   MPTCPPM
	resultMPTCPEv MPTCPEvent
)

// parseBenchCase is one parser P0 benchmarks: its name, the kind of its
// picks, and a call that decodes one message into the parser's result
// sink. The benchmarks, TestParseBenchRecords and TestParseBenchAllocs all
// read this one table, and TestParseBenchCases pins each parser's kind,
// since a parser can decode another type's body without an error.
type parseBenchCase struct {
	name  string
	kind  benchKind
	parse func(h NlMsgHdr, b []byte) error
}

var parseBenchCases = []parseBenchCase{
	{name: "ParseNewLink", kind: rtnl(unix.RTM_NEWLINK), parse: func(_ NlMsgHdr, b []byte) (err error) { resultLink, err = ParseNewLink(b); return err }},
	{name: "ParseNewRoute", kind: rtnl(unix.RTM_NEWROUTE), parse: func(_ NlMsgHdr, b []byte) (err error) { resultRoute, err = ParseNewRoute(b); return err }},
	{name: "ParseNewAddr", kind: rtnl(unix.RTM_NEWADDR), parse: func(_ NlMsgHdr, b []byte) (err error) { resultAddr, err = ParseNewAddr(b); return err }},
	{name: "ParseNeigh", kind: rtnl(unix.RTM_NEWNEIGH), parse: func(_ NlMsgHdr, b []byte) (err error) { resultNeigh, err = ParseNeigh(b); return err }},
	{name: "ParseRule", kind: rtnl(unix.RTM_NEWRULE), parse: func(_ NlMsgHdr, b []byte) (err error) { resultRule, err = ParseRule(b); return err }},
	{name: "ParseNewNexthop", kind: rtnl(unix.RTM_NEWNEXTHOP), parse: func(_ NlMsgHdr, b []byte) (err error) { resultNexthop, err = ParseNewNexthop(b); return err }},
	{name: "ParseNewAddrLabel", kind: rtnl(unix.RTM_NEWADDRLABEL), parse: func(_ NlMsgHdr, b []byte) (err error) { resultLabel, err = ParseNewAddrLabel(b); return err }},
	{name: "ParseNewNeighTbl", kind: rtnl(unix.RTM_NEWNEIGHTBL), parse: func(_ NlMsgHdr, b []byte) (err error) { resultNtbl, err = ParseNewNeighTbl(b); return err }},
	{name: "ParseNewNetconf", kind: rtnl(unix.RTM_NEWNETCONF), parse: func(_ NlMsgHdr, b []byte) (err error) { resultNetconf, err = ParseNewNetconf(b); return err }},
	{name: "ParseNewStats", kind: rtnl(unix.RTM_NEWSTATS), parse: func(_ NlMsgHdr, b []byte) (err error) { resultStats, err = ParseNewStats(b); return err }},
	{name: "ParseNlMsgErr", kind: rtnl(unix.NLMSG_ERROR), parse: func(h NlMsgHdr, b []byte) (err error) { resultNlErr, err = ParseNlMsgErr(h.Flags, b); return err }},
	{name: "ParseRtnetlinkEvent", kind: rtnl(unix.RTM_DELLINK), parse: func(h NlMsgHdr, b []byte) (err error) { resultEvent, err = ParseRtnetlinkEvent(h.Type, b); return err }},
	{name: "ParseGenericNetlink", kind: genl("nlctrl"), parse: func(_ NlMsgHdr, b []byte) (err error) { resultGenl, err = ParseGenericNetlink(b); return err }},
	{name: "ParseGenericNetlinkFamily", kind: genl("nlctrl"), parse: func(_ NlMsgHdr, b []byte) (err error) { resultFamily, err = ParseGenericNetlinkFamily(b); return err }},
	{name: "ParseEthtool", kind: genl("ethtool"), parse: func(h NlMsgHdr, b []byte) (err error) { resultEthtool, err = ParseEthtool(b, h.Flags); return err }},
	{name: "ParseNsid", kind: rtnl(unix.RTM_NEWNSID), parse: func(_ NlMsgHdr, b []byte) (err error) { resultNsid, err = ParseNsid(b); return err }},
	{name: "ParseNexthopBucket", kind: rtnl(unix.RTM_NEWNEXTHOPBUCKET), parse: func(_ NlMsgHdr, b []byte) (err error) { resultBucket, err = ParseNexthopBucket(b); return err }},
	{name: "ParseLinkProp", kind: rtnlRequest(unix.RTM_NEWLINKPROP), parse: func(_ NlMsgHdr, b []byte) (err error) { resultProp, err = ParseLinkProp(b); return err }},
	{name: "ParsePrefix", kind: rtnl(unix.RTM_NEWPREFIX), parse: func(_ NlMsgHdr, b []byte) (err error) { resultPrefix, err = ParsePrefix(b); return err }},
	{name: "ParseTCPMetrics", kind: genl(TCPMetricsGenlName), parse: func(_ NlMsgHdr, b []byte) (err error) { resultTCPM, err = ParseTCPMetrics(b); return err }},
	{name: "ParseSeg6", kind: genl(Seg6GenlName), parse: func(_ NlMsgHdr, b []byte) (err error) { resultSeg6, err = ParseSeg6(b); return err }},
	{name: "ParseIOAM6", kind: genlCmds(IOAM6GenlName, IOAM6CmdDumpNamespaces, IOAM6CmdDumpSchemas), parse: func(_ NlMsgHdr, b []byte) (err error) { resultIOAM6, err = ParseIOAM6(b); return err }},
	{name: "ParseIOAM6Event", kind: genlCmds(IOAM6GenlName, IOAM6EventTrace), parse: func(_ NlMsgHdr, b []byte) (err error) { resultIOAM6Ev, err = ParseIOAM6Event(b); return err }},
	{name: "ParseMPTCPPM", kind: genlAnswers(MPTCPPMGenlName), parse: func(_ NlMsgHdr, b []byte) (err error) { resultMPTCP, err = ParseMPTCPPM(b); return err }},
	{name: "ParseMPTCPEvent", kind: genlEvents(MPTCPPMGenlName), parse: func(_ NlMsgHdr, b []byte) (err error) { resultMPTCPEv, err = ParseMPTCPEvent(b); return err }},
}

// parseBenchCaseNamed returns the parseBenchCases entry called name.
func parseBenchCaseNamed(tb testing.TB, name string) parseBenchCase {
	tb.Helper()
	for _, c := range parseBenchCases {
		if c.name == name {
			return c
		}
	}
	tb.Fatalf("%s is not in parseBenchCases", name)
	return parseBenchCase{}
}

// benchParse runs the parser called name over its kind's three picks.
func benchParse(b *testing.B, name string) {
	c := parseBenchCaseNamed(b, name)
	for _, r := range benchRecords(b, c.kind) {
		b.Run(r.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := c.parse(r.h, r.body); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkParseNewLink decodes the smallest, a typical and the largest
// RTM_NEWLINK the corpus holds.
func BenchmarkParseNewLink(b *testing.B) { benchParse(b, "ParseNewLink") }

// BenchmarkParseNewRoute decodes the smallest, a typical and the largest
// RTM_NEWROUTE the corpus holds.
func BenchmarkParseNewRoute(b *testing.B) { benchParse(b, "ParseNewRoute") }

// BenchmarkParseNeigh decodes the smallest, a typical and the largest
// RTM_NEWNEIGH the corpus holds.
func BenchmarkParseNeigh(b *testing.B) { benchParse(b, "ParseNeigh") }

// BenchmarkParseRule decodes the smallest, a typical and the largest
// RTM_NEWRULE the corpus holds.
func BenchmarkParseRule(b *testing.B) { benchParse(b, "ParseRule") }

// BenchmarkParseNewNexthop decodes the smallest, a typical and the largest
// RTM_NEWNEXTHOP the corpus holds.
func BenchmarkParseNewNexthop(b *testing.B) { benchParse(b, "ParseNewNexthop") }

// BenchmarkParseNewAddrLabel decodes the smallest, a typical and the
// largest RTM_NEWADDRLABEL the corpus holds.
func BenchmarkParseNewAddrLabel(b *testing.B) { benchParse(b, "ParseNewAddrLabel") }

// BenchmarkParseNewNeighTbl decodes the smallest, a typical and the
// largest RTM_NEWNEIGHTBL the corpus holds.
func BenchmarkParseNewNeighTbl(b *testing.B) { benchParse(b, "ParseNewNeighTbl") }

// BenchmarkParseNewNetconf decodes the smallest, a typical and the largest
// RTM_NEWNETCONF the corpus holds.
func BenchmarkParseNewNetconf(b *testing.B) { benchParse(b, "ParseNewNetconf") }

// BenchmarkParseNewStats decodes the smallest, a typical and the largest
// RTM_NEWSTATS the corpus holds.
func BenchmarkParseNewStats(b *testing.B) { benchParse(b, "ParseNewStats") }

// BenchmarkParseNlMsgErr decodes the smallest, a typical and the largest
// rtnetlink NLMSG_ERROR the corpus holds.
func BenchmarkParseNlMsgErr(b *testing.B) { benchParse(b, "ParseNlMsgErr") }

// BenchmarkParseRtnetlinkEvent decodes the smallest, a typical and the
// largest RTM_DELLINK the corpus holds: a DEL type, since the NEW types
// are benchmarked through their own parsers.
func BenchmarkParseRtnetlinkEvent(b *testing.B) { benchParse(b, "ParseRtnetlinkEvent") }

// BenchmarkParseGenericNetlink decodes the smallest, a typical and the
// largest nlctrl message the corpus holds.
func BenchmarkParseGenericNetlink(b *testing.B) { benchParse(b, "ParseGenericNetlink") }

// BenchmarkParseGenericNetlinkFamily decodes the smallest, a typical and
// the largest nlctrl NEWFAMILY reply the corpus holds.
func BenchmarkParseGenericNetlinkFamily(b *testing.B) { benchParse(b, "ParseGenericNetlinkFamily") }

// BenchmarkParseEthtool decodes the smallest, a typical and the largest
// ethtool message the kernel sent in the corpus.
func BenchmarkParseEthtool(b *testing.B) { benchParse(b, "ParseEthtool") }

// BenchmarkParseNsid decodes the smallest, a typical and the largest
// RTM_NEWNSID the corpus holds (P1-T01).
func BenchmarkParseNsid(b *testing.B) { benchParse(b, "ParseNsid") }

// BenchmarkParseNexthopBucket decodes the smallest, a typical and the
// largest RTM_NEWNEXTHOPBUCKET the corpus holds (P1-T02).
func BenchmarkParseNexthopBucket(b *testing.B) { benchParse(b, "ParseNexthopBucket") }

// BenchmarkParseLinkProp decodes the smallest, a typical and the largest
// RTM_NEWLINKPROP in the corpus, all ip's requests, since the kernel sends
// none (P1-T03).
func BenchmarkParseLinkProp(b *testing.B) { benchParse(b, "ParseLinkProp") }

// BenchmarkParsePrefix decodes the smallest, a typical and the largest
// RTM_NEWPREFIX the corpus holds; all three are the same 44 bytes, the one
// shape the kernel sends (P1-T04).
func BenchmarkParsePrefix(b *testing.B) { benchParse(b, "ParsePrefix") }

// BenchmarkParseTCPMetrics decodes the smallest, a typical and the largest
// tcp_metrics entry the kernel sent in the corpus, an IPv4 and an IPv6 one
// (P2-T01).
func BenchmarkParseTCPMetrics(b *testing.B) { benchParse(b, "ParseTCPMetrics") }

// BenchmarkParseSeg6 decodes the smallest, a typical and the largest SEG6
// answer the kernel sent in the corpus: a tunnel source and HMAC keys
// (P2-T02).
func BenchmarkParseSeg6(b *testing.B) { benchParse(b, "ParseSeg6") }

// BenchmarkParseIOAM6 decodes the smallest, a typical and the largest
// namespace or schema the kernel dumped in the corpus (P2-T03).
func BenchmarkParseIOAM6(b *testing.B) { benchParse(b, "ParseIOAM6") }

// BenchmarkParseIOAM6Event decodes the corpus's one IOAM6 TRACE event, as
// every pick (P2-T03).
func BenchmarkParseIOAM6Event(b *testing.B) { benchParse(b, "ParseIOAM6Event") }

// BenchmarkParseMPTCPPM decodes the smallest, a typical and the largest
// mptcp_pm answer the kernel sent in the corpus, its limits and endpoints
// (P2-T04).
func BenchmarkParseMPTCPPM(b *testing.B) { benchParse(b, "ParseMPTCPPM") }

// BenchmarkParseMPTCPEvent decodes the smallest, a typical and the largest
// mptcp_pm event in the corpus (P2-T04).
func BenchmarkParseMPTCPEvent(b *testing.B) { benchParse(b, "ParseMPTCPEvent") }

func TestBenchPick(t *testing.T) {
	// msg is a captured message of type typ whose body is n bytes, from a
	// pcap named path.
	msg := func(path string, proto, typ, flags uint16, n int) corpusMsg {
		return corpusMsg{path: path, m: capturedNlMsg{proto: proto, h: NlMsgHdr{Type: typ, Flags: flags}, body: make([]byte, n)}}
	}
	route, newlink := uint16(unix.NETLINK_ROUTE), uint16(unix.RTM_NEWLINK)
	link := rtnl(newlink)
	// gmsg is a generic netlink message of a family, under the id it has
	// in its pcap.
	gmsg := func(path, family string, id uint16, n int) corpusMsg {
		c := msg(path, unix.NETLINK_GENERIC, id, 0, n)
		c.family = family
		return c
	}
	// gcmd is an IOAM6 message of command cmd.
	gcmd := func(path string, cmd uint8, n int) corpusMsg {
		c := gmsg(path, "IOAM6", 20, n)
		c.m.body[0] = cmd
		return c
	}
	// gpid is an mptcp_pm message the kernel sent to port pid.
	gpid := func(path string, pid uint32, n int) corpusMsg {
		c := gmsg(path, "mptcp_pm", 20, n)
		c.m.h.Pid = pid
		return c
	}
	// render shows each pick as name=path/length.
	render := func(recs []benchRecord) string {
		out := make([]string, 0, len(recs))
		for _, r := range recs {
			out = append(out, r.name+"="+r.path+"/"+strconv.Itoa(len(r.body)))
		}
		return strings.Join(out, " ")
	}
	tests := []struct {
		description string
		kind        benchKind
		msgs        []corpusMsg
		expected    string
	}{
		{
			description: "positive: five lengths give the first, the third and the fifth",
			kind:        link,
			msgs: []corpusMsg{
				msg("e", route, newlink, 0, 50), msg("a", route, newlink, 0, 10), msg("c", route, newlink, 0, 30),
				msg("b", route, newlink, 0, 20), msg("d", route, newlink, 0, 40),
			},
			expected: "smallest=a/10 typical=c/30 largest=e/50",
		},
		{
			description: "boundary: one message is every pick",
			kind:        link,
			msgs:        []corpusMsg{msg("a", route, newlink, unix.NLM_F_MULTI, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "boundary: two messages, the typical one the larger",
			kind:        link,
			msgs:        []corpusMsg{msg("a", route, newlink, 0, 52), msg("b", route, newlink, 0, 60)},
			expected:    "smallest=a/52 typical=b/60 largest=b/60",
		},
		{
			description: "corner: equal lengths keep corpus order",
			kind:        link,
			msgs:        []corpusMsg{msg("a", route, newlink, 0, 52), msg("b", route, newlink, 0, 52), msg("c", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=b/52 largest=c/52",
		},
		{
			description: "corner: ip's request is not a kernel message",
			kind:        link,
			msgs:        []corpusMsg{msg("req", route, newlink, unix.NLM_F_REQUEST|unix.NLM_F_ACK, 900), msg("a", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "positive: a request kind picks ip's requests, not the kernel's messages",
			kind:        rtnlRequest(newlink),
			msgs:        []corpusMsg{msg("req", route, newlink, unix.NLM_F_REQUEST|unix.NLM_F_ACK, 900), msg("a", route, newlink, 0, 52)},
			expected:    "smallest=req/900 typical=req/900 largest=req/900",
		},
		{
			description: "corner: the same type number in another protocol is not picked",
			kind:        link,
			msgs:        []corpusMsg{msg("xfrm", unix.NETLINK_XFRM, newlink, 0, 900), msg("a", route, newlink, 0, 52)},
			expected:    "smallest=a/52 typical=a/52 largest=a/52",
		},
		{
			description: "positive: a genl family is picked by name, whatever its id in each pcap",
			kind:        genl("ethtool"),
			msgs:        []corpusMsg{gmsg("a", "ethtool", 20, 40), gmsg("b", "ethtool", 21, 60), gmsg("ctrl", "nlctrl", GenlControllerID, 900)},
			expected:    "smallest=a/40 typical=b/60 largest=b/60",
		},
		{
			description: "corner: another family under the same id in another pcap is not picked",
			kind:        genl("ethtool"),
			msgs:        []corpusMsg{gmsg("a", "ethtool", 20, 40), gmsg("mptcp", "mptcp_pm", 20, 900)},
			expected:    "smallest=a/40 typical=a/40 largest=a/40",
		},
		{
			description: "positive: a command kind picks only those commands of its family",
			kind:        genlCmds("IOAM6", 3, 6),
			msgs:        []corpusMsg{gcmd("ev", 1, 900), gcmd("ns", 3, 12), gcmd("sc", 6, 20), gcmd("x", 4, 30)},
			expected:    "smallest=ns/12 typical=sc/20 largest=sc/20",
		},
		{
			description: "corner: an empty body has no command, and a command kind does not pick it",
			kind:        genlCmds("IOAM6", 0),
			msgs:        []corpusMsg{gmsg("empty", "IOAM6", 20, 0), gcmd("zero", 0, 4)},
			expected:    "smallest=zero/4 typical=zero/4 largest=zero/4",
		},
		{
			description: "positive: an answer kind picks the kernel's messages to a port, not its events",
			kind:        genlAnswers("mptcp_pm"),
			msgs:        []corpusMsg{gpid("ev", 0, 900), gpid("a", 7, 20), gpid("b", 8, 48)},
			expected:    "smallest=a/20 typical=b/48 largest=b/48",
		},
		{
			description: "positive: an event kind picks the kernel's multicast messages, nlmsg_pid 0",
			kind:        genlEvents("mptcp_pm"),
			msgs:        []corpusMsg{gpid("ev", 0, 28), gpid("a", 7, 20), gpid("b", 8, 900)},
			expected:    "smallest=ev/28 typical=ev/28 largest=ev/28",
		},
		{
			description: "negative: no message of the type",
			kind:        link,
			msgs:        []corpusMsg{msg("a", route, unix.RTM_NEWADDR, 0, 52)},
			expected:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := render(benchPick(tt.msgs, tt.kind)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseBenchCases(t *testing.T) {
	got := make([]string, 0, len(parseBenchCases))
	for _, c := range parseBenchCases {
		got = append(got, c.name+"="+c.kind.String())
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{
			description: "positive: each parser's picks are of its own kind",
			got:         strings.Join(got, " "),
			expected: "ParseNewLink=0/16 ParseNewRoute=0/24 ParseNewAddr=0/20 ParseNeigh=0/28 ParseRule=0/32 ParseNewNexthop=0/104" +
				" ParseNewAddrLabel=0/72 ParseNewNeighTbl=0/64 ParseNewNetconf=0/80 ParseNewStats=0/92" +
				" ParseNlMsgErr=0/2 ParseRtnetlinkEvent=0/17" +
				" ParseGenericNetlink=16/nlctrl ParseGenericNetlinkFamily=16/nlctrl ParseEthtool=16/ethtool" +
				" ParseNsid=0/88 ParseNexthopBucket=0/116 ParseLinkProp=0/108/request ParsePrefix=0/52" +
				" ParseTCPMetrics=16/tcp_metrics ParseSeg6=16/SEG6 ParseIOAM6=16/IOAM6/cmd3,6 ParseIOAM6Event=16/IOAM6/cmd1" +
				" ParseMPTCPPM=16/mptcp_pm/answers ParseMPTCPEvent=16/mptcp_pm/events",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}

// TestParseBenchRecords decodes every record a P0 benchmark runs over,
// so a benchmark never measures an error path.
func TestParseBenchRecords(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    string
	}{
		{description: "positive: ParseNewLink decodes its three RTM_NEWLINK picks", name: "ParseNewLink", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewRoute decodes its three RTM_NEWROUTE picks", name: "ParseNewRoute", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewAddr decodes its three RTM_NEWADDR picks", name: "ParseNewAddr", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNeigh decodes its three RTM_NEWNEIGH picks", name: "ParseNeigh", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseRule decodes its three RTM_NEWRULE picks", name: "ParseRule", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNexthop decodes its three RTM_NEWNEXTHOP picks", name: "ParseNewNexthop", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewAddrLabel decodes its three RTM_NEWADDRLABEL picks", name: "ParseNewAddrLabel", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNeighTbl decodes its three RTM_NEWNEIGHTBL picks", name: "ParseNewNeighTbl", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewNetconf decodes its three RTM_NEWNETCONF picks", name: "ParseNewNetconf", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNewStats decodes its three RTM_NEWSTATS picks", name: "ParseNewStats", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNlMsgErr decodes its three rtnetlink NLMSG_ERROR picks", name: "ParseNlMsgErr", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseRtnetlinkEvent decodes its three RTM_DELLINK picks", name: "ParseRtnetlinkEvent", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseGenericNetlink decodes its three nlctrl picks", name: "ParseGenericNetlink", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseGenericNetlinkFamily decodes its three nlctrl picks", name: "ParseGenericNetlinkFamily", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseEthtool decodes its three ethtool picks", name: "ParseEthtool", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNsid decodes its three RTM_NEWNSID picks", name: "ParseNsid", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseNexthopBucket decodes its three RTM_NEWNEXTHOPBUCKET picks", name: "ParseNexthopBucket", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseLinkProp decodes its three picks of ip's RTM_NEWLINKPROP requests", name: "ParseLinkProp", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParsePrefix decodes its three RTM_NEWPREFIX picks", name: "ParsePrefix", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseTCPMetrics decodes its three tcp_metrics picks", name: "ParseTCPMetrics", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseSeg6 decodes its three SEG6 picks", name: "ParseSeg6", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseIOAM6 decodes its three IOAM6 dump picks", name: "ParseIOAM6", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseIOAM6Event decodes its IOAM6 TRACE pick", name: "ParseIOAM6Event", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseMPTCPPM decodes its three mptcp_pm answer picks", name: "ParseMPTCPPM", expected: "smallest ok, typical ok, largest ok"},
		{description: "positive: ParseMPTCPEvent decodes its three mptcp_pm event picks", name: "ParseMPTCPEvent", expected: "smallest ok, typical ok, largest ok"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			c := parseBenchCaseNamed(t, tt.name)
			recs := benchRecords(t, c.kind)
			got := make([]string, 0, len(recs))
			for i, r := range recs {
				status := "ok"
				if err := c.parse(r.h, r.body); err != nil {
					status = err.Error()
				}
				if i > 0 && len(r.body) < len(recs[i-1].body) {
					status = "shorter than " + recs[i-1].name
				}
				got = append(got, r.name+" "+status)
			}
			if s := strings.Join(got, ", "); s != tt.expected {
				t.Errorf("got %q, expected %q", s, tt.expected)
			}
		})
	}
}

// TestParseBenchAllocs asserts the allocations of each parser that is
// allocation-free on some of its picks today, so a change that makes it
// allocate fails here, not only in a benchmark nobody reads. Each result
// leads with the parser's kind, since two parsers can allocate alike.
func TestParseBenchAllocs(t *testing.T) {
	tests := []struct {
		description string
		name        string
		expected    string
	}{
		{description: "corner: ParseRule allocates nothing on any pick", name: "ParseRule", expected: "0/32: smallest 0, typical 0, largest 0"},
		{description: "corner: ParseNewStats allocates nothing on its header-only picks, and once on the largest", name: "ParseNewStats", expected: "0/92: smallest 0, typical 0, largest 1"},
		{description: "corner: ParseNlMsgErr allocates nothing on its capped picks, and once on the largest, whose extended-ack TLVs it decodes", name: "ParseNlMsgErr", expected: "0/2: smallest 0, typical 0, largest 1"},
		{description: "corner: ParseNsid allocates nothing on any pick", name: "ParseNsid", expected: "0/88: smallest 0, typical 0, largest 0"},
		{description: "corner: ParseNexthopBucket allocates nothing on any pick", name: "ParseNexthopBucket", expected: "0/116: smallest 0, typical 0, largest 0"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			c := parseBenchCaseNamed(t, tt.name)
			recs := benchRecords(t, c.kind)
			got := make([]string, 0, len(recs))
			for _, r := range recs {
				n := testing.AllocsPerRun(1_000, func() {
					if err := c.parse(r.h, r.body); err != nil {
						t.Fatal(err)
					}
				})
				got = append(got, r.name+" "+strconv.FormatFloat(n, 'f', -1, 64))
			}
			if s := c.kind.String() + ": " + strings.Join(got, ", "); s != tt.expected {
				t.Errorf("got %q, expected %q", s, tt.expected)
			}
		})
	}
}
