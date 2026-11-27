//go:build gotc_write

package gotc

import (
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// q_fifo.c fifo_parse_opt and q_prio.c prio_parse_opt at iproute2 v7.2.0.

// fifoParser is fifo_parse_opt for the kind id, which its messages name
// (qu->id): pfifo, bfifo and pfifo_head_drop share it.
func fifoParser(id string) kindParser {
	return func(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
		ok := 0
		var opt xtcpnl.TcFifoQopt
		for ; len(args) > 0; args = args[1:] {
			switch args[0] {
			case wordLimit:
				if len(args) < 2 {
					return nil, r.incomplete()
				}
				args = args[1:]
				v, good := getSize(args[0])
				if !good {
					r.errf("%s: Illegal value for \"limit\": \"%s\"\n", id, args[0])
					return nil, errKindParse
				}
				opt.Limit = v
				ok++
			case helpWord:
				r.fifoExplain()
				return nil, errKindParse
			default:
				r.errf("%s: unknown parameter \"%s\"\n", id, args[0])
				r.fifoExplain()
				return nil, errKindParse
			}
		}
		if ok == 0 {
			return nil, nil
		}
		return func(ab *xtcpnl.AttrBuilder) error {
			var b [xtcpnl.TcFifoQoptSizeCst]byte
			return ab.PutBytes(xtcpnl.TcaOptions, opt.Append(b[:0]))
		}, nil
	}
}

func (r *runner) fifoExplain() {
	r.errf("Usage: ... <[p|b]fifo | pfifo_head_drop> [ limit NUMBER ]\n")
}

// tcPrioMax is TC_PRIO_MAX.
const tcPrioMax = 15

// parsePrioQopt is prio_parse_opt. A band is checked against bands as
// unsigned (`band >= opt.bands`), so a negative bands admits any band.
func parsePrioQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	pmap, idx, mq := false, 0, false
	opt := xtcpnl.TcPrioQopt{Bands: 3, Priomap: [16]uint8{1, 2, 2, 2, 1, 2, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1}}
	for ; len(args) > 0; args = args[1:] {
		switch a := args[0]; a {
		case wordBands:
			if pmap {
				r.prioExplain()
			}
			if len(args) < 2 {
				return nil, r.incomplete()
			}
			args = args[1:]
			v, ok := getInteger(args[0], 10)
			if !ok {
				r.errf("Illegal \"bands\"\n")
				return nil, errKindParse
			}
			opt.Bands = v
		case "priomap":
			if pmap {
				r.errf("Error: duplicate priomap\n")
				return nil, errKindParse
			}
			pmap = true
		case "multiqueue":
			mq = true
		case helpWord:
			r.prioExplain()
			return nil, errKindParse
		default:
			if !pmap {
				r.errf("What is \"%s\"?\n", a)
				r.prioExplain()
				return nil, errKindParse
			}
			band, ok := getU32(a, 10) // get_unsigned
			if !ok {
				r.errf("Illegal \"priomap\" element\n")
				return nil, errKindParse
			}
			if band >= cint.Bits(opt.Bands) {
				r.errf("\"priomap\" element is out of bands\n")
				return nil, errKindParse
			}
			if idx > tcPrioMax {
				r.errf("\"priomap\" index > TC_PRIO_MAX=%d\n", tcPrioMax)
				return nil, errKindParse
			}
			opt.Priomap[idx] = uint8(band & 0xff)
			idx++
		}
	}
	return func(ab *xtcpnl.AttrBuilder) error {
		var b [xtcpnl.TcPrioQoptSizeCst]byte
		nest, err := ab.BeginNestCompat(xtcpnl.TcaOptions, opt.Append(b[:0]))
		if err != nil {
			return err
		}
		if mq {
			if err := ab.PutBytes(xtcpnl.TcaPrioMQ, nil); err != nil {
				return err
			}
		}
		return ab.EndNestCompat(nest)
	}, nil
}

func (r *runner) prioExplain() {
	r.errf("Usage: ... prio bands NUMBER priomap P1 P2...[multiqueue]\n")
}
