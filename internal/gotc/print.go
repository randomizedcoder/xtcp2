package gotc

import (
	"bufio"
)

// This is tc's output layer: lib/json_print.c and lib/json_writer.c under
// it. 9S carries the state the option loop sets; the printers arrive with
// the shows that call them.

// out is tc's output state: stdout, the -j/-p/-o globals and color.
type out struct {
	w      *bufio.Writer
	err    error
	json   bool
	pretty bool
	sl     string // _SL_: "\n", or "\\" under -oneline
	color  bool
}

func newOut(w *bufio.Writer) *out {
	return &out{w: w, sl: "\n"}
}

func (o *out) raw(s string) {
	if _, err := o.w.WriteString(s); err != nil && o.err == nil {
		o.err = err
	}
}

func (o *out) printf(s string) { o.raw(s) }
