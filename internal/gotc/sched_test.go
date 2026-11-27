package gotc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The increment-15 kinds (drr, qfq, hfsc, ets, mqprio, taprio, multiq, cbs,
// etf) against scripted replies: one row per lead the tcsched corpus pins,
// so a mutant that breaks one fails here by name, and the crafted paths
// the kernel never sends (short attributes, num_tc past 16, 17 rates).
//
// go test ./internal/gotc/ -run TestGotcSched

// nestOpts is a TCA_OPTIONS nest; rawOpts a TCA_OPTIONS of raw bytes.
func nestOpts(kids ...[]byte) []byte { return attr(xtcpnl.TcaOptions|unix.NLA_F_NESTED, cat(kids...)) }
func rawOpts(b []byte) []byte        { return attr(xtcpnl.TcaOptions, b) }

// schedQ is a qdisc 1: at the root of d0 with refcnt 2.
func schedQ(k string, a ...[]byte) Msg {
	return qmsg(tcBody(2, 0x10000, tcHRoot, 2, append([][]byte{kind(k)}, a...)...))
}

// schedC is class 1:minor of kind k under parent.
func schedC(k string, minor, parent uint32, a ...[]byte) Msg {
	return cmsg(tcBody(2, 0x10000|minor, parent, 0, append([][]byte{kind(k)}, a...)...))
}

func showD0(m ...Msg) []Reply {
	return []Reply{rep(linkMsg(2, "d0")), rep(append(m, done())...)}
}

// mqprioQopt is a struct tc_mqprio_qopt: num_tc, map {0 0 1 1}, and
// counts and offsets for the first two classes.
func mqprioQopt(numTc uint8, c0, o0, c1, o1 uint16) []byte {
	b := make([]byte, xtcpnl.TcMqprioQoptSizeCst)
	b[0] = numTc
	b[3], b[4] = 1, 1
	copy(b[18:], le16(c0))
	copy(b[20:], le16(c1))
	copy(b[50:], le16(o0))
	copy(b[52:], le16(o1))
	return b
}

// mqprioTail is the header padded to RTA_ALIGN, then attrs.
func mqprioTail(hdr []byte, attrs ...[]byte) []byte { return cat(hdr, []byte{0, 0}, cat(attrs...)) }

// tcEntry is a TCA_*_TC_ENTRY nest: index, then the extra attributes.
func tcEntry(entry uint16, index uint32, extra ...[]byte) []byte {
	return attr(entry|unix.NLA_F_NESTED, cat(attr(1, le32(index)), cat(extra...)))
}

// mqprioEntries is the 16 entries the kernel always sends, fp express.
func mqprioEntries() []byte {
	var b []byte
	for i := range uint32(16) {
		b = cat(b, tcEntry(xtcpnl.TcaMqprioTcEntry, i, attr(xtcpnl.TcaMqprioTcEntryFp, le32(xtcpnl.TcFpExpress))))
	}
	return b
}

func mqprioRates(t uint16, n int) []byte {
	var kids []byte
	for i := range n {
		kids = cat(kids, attr(t, le64(uint64(125000*(i+1)))))
	}
	return attr(t, kids) // nla_nest_start_noflag
}

// sc is a struct tc_service_curve.
func sc(m1, d, m2 uint32) []byte { return cat(le32(m1), le32(d), le32(m2)) }

// taprioOpts is a taprio TCA_OPTIONS: the priomap (num_tc 2, 4@0 4@4) and
// the given attributes, then 2 tc entries with max-sdu and fp.
func taprioOpts(numTc uint8, a ...[]byte) []byte {
	ents := cat(
		tcEntry(xtcpnl.TcaTaprioAttrTcEntry, 0, attr(xtcpnl.TcaTaprioTcEntryMaxSdu, le32(0)), attr(xtcpnl.TcaTaprioTcEntryFp, le32(1))),
		tcEntry(xtcpnl.TcaTaprioAttrTcEntry, 1, attr(xtcpnl.TcaTaprioTcEntryMaxSdu, le32(1500)), attr(xtcpnl.TcaTaprioTcEntryFp, le32(2))))
	return nestOpts(attr(xtcpnl.TcaTaprioAttrPriomap, mqprioQopt(numTc, 4, 0, 4, 4)), cat(a...), ents)
}

// taprioEntry is one schedule entry.
func taprioEntry(index uint32, cmd uint8, gate, interval uint32) []byte {
	return attr(1|unix.NLA_F_NESTED, cat(attr(xtcpnl.TcaTaprioSchedEntryIndex, le32(index)),
		attr(xtcpnl.TcaTaprioSchedEntryCmd, []byte{cmd}), attr(xtcpnl.TcaTaprioSchedEntryGateMask, le32(gate)),
		attr(xtcpnl.TcaTaprioSchedEntryInterval, le32(interval))))
}

func taprioSched(base, cycle int64, ents ...[]byte) []byte {
	return cat(attr(xtcpnl.TcaTaprioAttrSchedBaseTime, le64(uint64(base))),
		attr(xtcpnl.TcaTaprioAttrSchedCycleTime, le64(uint64(cycle))),
		attr(xtcpnl.TcaTaprioAttrSchedCycleTimeExtension, le64(0)),
		attr(xtcpnl.TcaTaprioAttrSchedEntryList|unix.NLA_F_NESTED, cat(ents...)))
}

func etsOpts(quanta ...[]byte) []byte {
	pm := make([][]byte, 16)
	for i := range pm {
		pm[i] = attr(xtcpnl.TcaEtsPriomapBand, []byte{2})
	}
	pm[1] = attr(xtcpnl.TcaEtsPriomapBand, []byte{1})
	pm[2] = attr(xtcpnl.TcaEtsPriomapBand, []byte{0})
	return nestOpts(attr(xtcpnl.TcaEtsNbands, []byte{3}), attr(xtcpnl.TcaEtsNstrict, []byte{1}),
		attr(xtcpnl.TcaEtsQuanta|unix.NLA_F_NESTED, cat(quanta...)),
		attr(xtcpnl.TcaEtsPriomap|unix.NLA_F_NESTED, cat(pm...)))
}

func band(q uint32) []byte { return attr(xtcpnl.TcaEtsQuantaBand, le32(q)) }

func hfscStats(work, rtwork uint64, period, level uint32) []byte {
	return stats2(basic(0, 0), attr(xtcpnl.TcaStatsApp, cat(le64(work), le64(rtwork), le32(period), le32(level))))
}

const fp16E = " E E E E E E E E E E E E E E E E"

func TestGotcSched(t *testing.T) {
	drrClass := schedC("drr", 1, tcHRoot, nestOpts(attr(xtcpnl.TcaDrrQuantum, le32(1514))),
		stats2(basic(0, 0), attr(xtcpnl.TcaStatsApp, le32(0))))
	hfscQ := schedQ("hfsc", rawOpts(le16(0x10)))
	mqprio := schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(2, 4, 0, 4, 4), mqprioEntries())))
	taprio := schedQ("taprio", taprioOpts(2, attr(xtcpnl.TcaTaprioAttrSchedClockid, le32(11)),
		attr(xtcpnl.TcaTaprioAttrFlags, le32(1)), attr(xtcpnl.TcaTaprioAttrTxtimeDelay, le32(200000)),
		taprioSched(1000000000, 500000, taprioEntry(0, 0, 1, 300000), taprioEntry(1, 1, 2, 200000)),
		attr(xtcpnl.TcaTaprioAttrAdminSched|unix.NLA_F_NESTED, taprioSched(4000000000000000000, 100000, taprioEntry(0, 2, 3, 100000)))))
	taprioText := "qdisc taprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0\n" +
		"queues offset 0 count 4 offset 4 count 4\n"
	tsn := []Msg{
		schedQ("cbs", nestOpts(attr(xtcpnl.TcaCbsParms, cat([]byte{0, 0, 0, 0}, le32(30), le32(0xfffffa42), le32(20000), le32(0xfff10be0))))),
		schedQ("etf", nestOpts(attr(xtcpnl.TcaEtfParms, cat(le32(300000), le32(11), le32(xtcpnl.TcEtfSkipSockCheckOn))))),
	}
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    result
	}{
		// Positive.
		{"positive: drr class quantum and deficit in text (Q37, Q38)", []string{"-s", "class", "show", "dev", "d0"},
			showD0(drrClass), result{stdout: "class drr 1:1 root quantum 1514b \n Sent 0 bytes 0 pkt\n deficit 0b \n"}},
		{"positive: drr quantum and deficit absent in -j (Q37, Q38)", []string{"-j", "-s", "class", "show", "dev", "d0"},
			showD0(drrClass), result{stdout: `[{"class":"drr","handle":"1:1","root":true,"stats":{"bytes":0,"packets":0}}]` + "\n"}},
		{"positive: qfq class, its xstats unprinted (Q39)", []string{"-s", "class", "show", "dev", "d0"},
			showD0(schedC("qfq", 1, tcHRoot, nestOpts(attr(xtcpnl.TcaQfqWeight, le32(1)), attr(xtcpnl.TcaQfqLmax, le32(1514))),
				stats2(basic(0, 0), attr(xtcpnl.TcaStatsApp, cat(le32(1), le32(1514)))))),
			result{stdout: "class qfq 1:1 root weight 1 maxpkt 1514 \n Sent 0 bytes 0 pkt\n"}},
		{"positive: hfsc default 10, two spaces (Q40)", []string{"qdisc", "show", "dev", "d0"},
			showD0(hfscQ), result{stdout: "qdisc hfsc 1: root refcnt 2  default 0x10 \n"}},
		{"positive: hfsc default as a JSON string (Q40)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(hfscQ), result{stdout: `[{"kind":"hfsc","handle":"1:","root":true,"refcnt":2,"options":{"default":"0x10"}}]` + "\n"}},
		{"positive: hfsc rt equal to ls is one sc (Q43)", []string{"class", "show", "dev", "d0"},
			showD0(schedC("hfsc", 0x20, 0x10001, nestOpts(attr(xtcpnl.TcaHfscRsc, sc(0, 0, 125000)), attr(xtcpnl.TcaHfscFsc, sc(0, 0, 125000))))),
			result{stdout: "class hfsc 1:20 parent 1:1 sc m1 0bit d 0us m2 1Mbit \n"}},
		{"positive: hfsc rt, ls and ul, d in us in JSON (Q44)", []string{"-j", "class", "show", "dev", "d0"},
			showD0(schedC("hfsc", 0x10, 0x10001, nestOpts(attr(xtcpnl.TcaHfscRsc, sc(250000, 10000, 125000)),
				attr(xtcpnl.TcaHfscFsc, sc(0, 0, 375000)), attr(xtcpnl.TcaHfscUsc, sc(0, 0, 500000))))),
			result{stdout: `[{"class":"hfsc","handle":"1:10","parent":"1:1","rt":{"m1":250000,"d":10000,"m2":125000},` +
				`"ls":{"m1":0,"d":0,"m2":375000},"ul":{"m1":0,"d":0,"m2":500000}}]` + "\n"}},
		{"positive: ets bands 3 strict 1, arrays ending in a space (Q45)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("ets", etsOpts(band(1500), band(3000)))),
			result{stdout: "qdisc ets 1: root refcnt 2 bands 3 strict 1 quanta 1500 3000 priomap 2 1 0 2 2 2 2 2 2 2 2 2 2 2 2 2 \n"}},
		{"positive: ets classes, the strict one without a quantum (Q46)", []string{"-j", "class", "show", "dev", "d0"},
			showD0(schedC("ets", 1, tcHRoot, nestOpts()), schedC("ets", 2, tcHRoot, nestOpts(band(1500)))),
			result{stdout: `[{"class":"ets","handle":"1:1","root":true},{"class":"ets","handle":"1:2","root":true,"quantum":1500}]` + "\n"}},
		{"positive: mqprio hw 0: dcb mode and shaper, 16 fp, a blank line (Q49, Q50)", []string{"qdisc", "show", "dev", "d0"},
			showD0(mqprio), result{stdout: "qdisc mqprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n" +
				"             queues:(0:3) (4:7) \n             mode:dcb\n             shaper:dcb\n             fp:" + fp16E + "\n\n"}},
		{"positive: mqprio JSON queues as pairs, map without its space (Q51, Q52)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(2, 4, 0, 4, 4), attr(xtcpnl.TcaMqprioMode, le16(1)))))),
			result{stdout: `[{"kind":"mqprio","handle":"1:","root":true,"refcnt":2,"options":{"tc":2,"map":[0,0,1,1,0,0,0,0,0,0,0,0,0,0,0,0],` +
				`"queues":[[0,3],[4,7]],"mode":"channel","shaper":"dcb"}}]` + "\n"}},
		{"positive: taprio oper and an unlabelled admin schedule (Q55, Q57, Q58)", []string{"qdisc", "show", "dev", "d0"},
			showD0(taprio), result{stdout: taprioText + "clockid TAI flags 0x1 txtime delay 200000" +
				"\tbase-time 1000000000 cycle-time 500000 cycle-time-extension 0\n" +
				"\tindex 0 cmd S gatemask 0x1 interval 300000\n\tindex 1 cmd H gatemask 0x2 interval 200000\n" +
				"\tbase-time 4000000000000000000 cycle-time 100000 cycle-time-extension 0\n" +
				"\tindex 0 cmd R gatemask 0x3 interval 100000\nmax-sdu 0 1500\nfp E P\n\n"}},
		{"positive: taprio JSON max-sdu with a hyphen, admin as an object (Q56)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(taprio), result{stdout: `[{"kind":"taprio","handle":"1:","root":true,"refcnt":2,"options":{"tc":2,` +
				`"map":[0,0,1,1,0,0,0,0,0,0,0,0,0,0,0,0],"queues":[{"offset":0,"count":4},{"offset":4,"count":4}],` +
				`"clockid":"TAI","flags":"0x1","txtime_delay":200000,"base_time":1000000000,"cycle_time":500000,"cycle_time_extension":0,` +
				`"schedule":[{"index":0,"cmd":"S","gatemask":"0x1","interval":300000},{"index":1,"cmd":"H","gatemask":"0x2","interval":200000}],` +
				`"admin":{"base_time":4000000000000000000,"cycle_time":100000,"cycle_time_extension":0,` +
				`"schedule":[{"index":0,"cmd":"R","gatemask":"0x3","interval":100000}]},"max-sdu":[0,1500],"fp":["E","P"]}}]` + "\n"}},
		{"positive: multiq bands 8/8 (Q60)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("multiq", rawOpts(cat(le16(8), le16(8))))), result{stdout: "qdisc multiq 1: root refcnt 2 bands 8/8 \n"}},
		{"positive: cbs and etf text (Q63)", []string{"qdisc", "show", "dev", "d0"}, showD0(tsn...),
			result{stdout: "qdisc cbs 1: root refcnt 2 hicredit 30 locredit -1470 sendslope -980000 idleslope 20000 offload 0 \n" +
				"qdisc etf 1: root refcnt 2 clockid TAI delta 300000 offload off deadline_mode off skip_sock_check on\n"}},
		{"positive: cbs offload a number, etf's a string (Q61)", []string{"-j", "qdisc", "show", "dev", "d0"}, showD0(tsn...),
			result{stdout: `[{"kind":"cbs","handle":"1:","root":true,"refcnt":2,"options":{"hicredit":30,"locredit":-1470,"sendslope":-980000,` +
				`"idleslope":20000,"offload":0}},{"kind":"etf","handle":"1:","root":true,"refcnt":2,"options":{"clockid":"TAI","delta":300000,` +
				`"offload":"off","deadline_mode":"off","skip_sock_check":"on"}}]` + "\n"}},
		// Negative.
		{"negative: an ets quanta list short of a band stops, its array open (Q47)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("ets", etsOpts(band(1500), attr(xtcpnl.TcaEtsPriomapBand, []byte{1})))),
			result{stdout: "qdisc ets 1: root refcnt 2 bands 3 strict 1 quanta 1500\n", stderr: "No ETS band quantum\n"}},
		{"negative: an ets quanta list short of a band leaves -j's array open (Q47)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(schedQ("ets", etsOpts(band(1500), attr(xtcpnl.TcaEtsPriomapBand, []byte{1})))),
			result{stdout: `[{"kind":"ets","handle":"1:","root":true,"refcnt":2,"options":{"bands":3,"strict":1,"quanta":[1500}}]` + "\n",
				stderr: "No ETS band quantum\n"}},
		{"negative: a 1-byte mqprio mode is refused (Q54)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(2, 4, 0, 4, 4), attr(xtcpnl.TcaMqprioMode, []byte{1}))))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n             queues:(0:3) (4:7) ",
				stderr: "gotc: not implemented: mqprio mode of 1 bytes (tc reads it unchecked)\n", rc: 255}},
		{"negative: a short taprio PRIOMAP is refused (Q54)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("taprio", nestOpts(attr(xtcpnl.TcaTaprioAttrPriomap, make([]byte, 20))))),
			result{stdout: "qdisc taprio 1: root refcnt 2 ",
				stderr: "gotc: not implemented: taprio parameters of 20 bytes (tc reads it unchecked)\n", rc: 255}},
		{"negative: red, an increment-16 kind, is refused by name", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("red")), result{stderr: "gotc: not implemented: qdisc kind \"red\"\n", rc: 255}},
		{"negative: mqprio options short of RTA_ALIGN(sizeof)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioQopt(2, 4, 0, 4, 4)))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 \n", stderr: "options size error\n"}},
		// Boundary.
		{"boundary: an mqprio queue of count 0 ends at 4294967295 (Q51)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(1, 0, 0, 0, 0))))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 tc 1 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n             queues:(0:4294967295) \n"}},
		{"boundary: 17 mqprio min rates stop at 16, num_tc printed (Q53)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(2, 4, 0, 4, 4), attr(xtcpnl.TcaMqprioShaper, le16(1)),
				mqprioRates(xtcpnl.TcaMqprioMinRate64, 17))))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n             queues:(0:3) (4:7) \n" +
				"             mode:dcb\n             shaper:bw_rlimit\tmin_rate:1Mbit 2Mbit \n"}},
		{"boundary: an mqprio num_tc of 17 is refused", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(17, 4, 0, 4, 4))))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 ",
				stderr: "gotc: not implemented: mqprio num_tc 17 (tc reads it unchecked)\n", rc: 255}},
		{"boundary: a tc entry index of 16 is tc's message, skipped", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("mqprio", rawOpts(mqprioTail(mqprioQopt(2, 4, 0, 4, 4),
				tcEntry(xtcpnl.TcaMqprioTcEntry, 16, attr(xtcpnl.TcaMqprioTcEntryFp, le32(2))))))),
			result{stdout: "qdisc mqprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n             queues:(0:3) (4:7) \n" +
				"             mode:dcb\n             shaper:dcb\n", stderr: "Unexpected tc entry index 16\n"}},
		// Corner.
		{"corner: mqprio's hard newlines survive -o (Q48)", []string{"-o", "qdisc", "show", "dev", "d0"},
			showD0(mqprio), result{stdout: "qdisc mqprio 1: root refcnt 2 tc 2 map 0 0 1 1 0 0 0 0 0 0 0 0 0 0 0 0 \n" +
				"             queues:(0:3) (4:7) \n             mode:dcb\n             shaper:dcb\n             fp:" + fp16E + "\\\\"}}, // print_nl twice: _SL_ is "\\"
		{"corner: taprio TXTIME_DELAY 0x80000000 is signed in text (Q55)", []string{"qdisc", "show", "dev", "d0"},
			showD0(schedQ("taprio", taprioOpts(2, attr(xtcpnl.TcaTaprioAttrTxtimeDelay, le32(0x80000000))))),
			result{stdout: taprioText + " txtime delay -2147483648max-sdu 0 1500\nfp E P\n\n"}},
		{"corner: taprio TXTIME_DELAY 0x80000000 is unsigned in JSON (Q55)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(schedQ("taprio", taprioOpts(2, attr(xtcpnl.TcaTaprioAttrTxtimeDelay, le32(0x80000000))))),
			result{stdout: `[{"kind":"taprio","handle":"1:","root":true,"refcnt":2,"options":{"tc":2,"map":[0,0,1,1,0,0,0,0,0,0,0,0,0,0,0,0],` +
				`"queues":[{"offset":0,"count":4},{"offset":4,"count":4}],"txtime_delay":2147483648,"max-sdu":[0,1500],"fp":["E","P"]}}]` + "\n"}},
		{"corner: a negative base-time, signed in text, unsigned in JSON (Q59)", []string{"-j", "qdisc", "show", "dev", "d0"},
			showD0(schedQ("taprio", taprioOpts(2, attr(xtcpnl.TcaTaprioAttrSchedBaseTime, le64(0xffffffffffffffff))))),
			result{stdout: `[{"kind":"taprio","handle":"1:","root":true,"refcnt":2,"options":{"tc":2,"map":[0,0,1,1,0,0,0,0,0,0,0,0,0,0,0,0],` +
				`"queues":[{"offset":0,"count":4},{"offset":4,"count":4}],"base_time":18446744073709551615,"max-sdu":[0,1500],"fp":["E","P"]}}]` + "\n"}},
		{"corner: hfsc -s: work only when nonzero, then a blank line (Q41, Q42)", []string{"-s", "class", "show", "dev", "d0"},
			showD0(schedC("hfsc", 1, tcHRoot, hfscStats(0, 0, 0, 2)), schedC("hfsc", 2, tcHRoot, hfscStats(1500, 0, 3, 0))),
			result{stdout: "class hfsc 1:1 root \n Sent 0 bytes 0 pkt\n period 0  level 2 \n\n" +
				"class hfsc 1:2 root \n Sent 0 bytes 0 pkt\n period 3 work 1500 bytes  level 0 \n\n"}},
		{"corner: -g class show prints hfsc's curves", []string{"-g", "class", "show", "dev", "d0"},
			showD0(schedC("hfsc", 0x10, tcHRoot, nestOpts(attr(xtcpnl.TcaHfscRsc, sc(0, 0, 125000))))),
			result{stdout: "+---(1:10) hfsc rt m1 0bit d 0us m2 1Mbit \n\n"}},
		{"corner: -d prints taprio's size table overhead", []string{"-d", "qdisc", "show", "dev", "d0"},
			showD0(schedQ("multiq", rawOpts(cat(le16(8), le16(8))),
				attr(xtcpnl.TcaStab|unix.NLA_F_NESTED, attr(xtcpnl.TcaStabBase, cat([]byte{0, 0, 0, 0}, le32(24), le32(0), le32(0), le32(0), le32(0)))))),
			result{stdout: "qdisc multiq 1: root refcnt 2 bands 8/8 \n overhead 24 \n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}
