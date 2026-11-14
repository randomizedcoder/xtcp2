package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// row is one line of a rows file. The field set and its checks mirror
// nix/microvms/workbench/nlcap_batch.py check_row, which refuses a whole
// file over one bad row; validate catches the same mistakes here first.
type row struct {
	ID       string     `json:"id"`
	Desc     string     `json:"desc"`
	Cmd      string     `json:"cmd"`
	Pre      []preStep  `json:"pre,omitempty"`
	Readback []readback `json:"readback,omitempty"`
	Modules  []string   `json:"modules,omitempty"`
	// Proto is the netlink protocol the command is captured on; "" is
	// NETLINK_ROUTE.
	Proto  string `json:"proto,omitempty"`
	Serial bool   `json:"serial,omitempty"`
	Expect expect `json:"expect"`
	// Axes is what a generated row's values mean (matrix.go's facts), for
	// the corpus test's model of the request to read.
	Axes   map[string]string `json:"axes,omitempty"`
	Source *source           `json:"source,omitempty"`
}

// preStep is a setup command run outside the capture window. OK lists the
// exit statuses besides 0 that it may end with; it is written as a bare
// string when there are none and as [cmd, [rcs]] otherwise.
type preStep struct {
	Cmd string
	OK  []int
}

func (p preStep) MarshalJSON() ([]byte, error) {
	var v any = p.Cmd
	if len(p.OK) > 0 {
		v = []any{p.Cmd, p.OK}
	}
	// Shell commands are full of & and >; keep them readable in the file.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

type readback struct {
	Name string `json:"name"`
	Cmd  string `json:"cmd"`
	// Proto is the protocol the readback is captured on, as row.Proto.
	Proto string `json:"proto,omitempty"`
}

// expect is what the run must show; see wsExpect in
// pkg/xtcpnl/xtcpnl_ws_corpus_test.go for the meaning of each field.
type expect struct {
	RC     *int   `json:"rc,omitempty"`
	Errno  string `json:"errno,omitempty"`
	Extack string `json:"extack,omitempty"`
	// Sent is how many requests the command sends, for a row whose
	// command is not a netlink tool (a ping sends none, so who refused
	// it can't be read from the capture).
	Sent      *int    `json:"sent,omitempty"`
	RefusedBy refuser `json:"refused_by,omitempty"`
	Match     *match  `json:"match,omitempty"`
}

// refuser is who refuses a row, as wsExpect's refused_by reads it.
type refuser string

const (
	// refNone is a row nothing refuses.
	refNone refuser = "none"
	// refIproute2 is a refusal ip makes before it sends.
	refIproute2 refuser = "iproute2"
	// refKernel is one the kernel answers with an errno.
	refKernel refuser = "kernel"
	// refKernelIgnored is a request the kernel acks without acting on.
	refKernelIgnored refuser = "kernel-ignored"
	// refAckThenFail is a kernel ack followed by ip's own failure.
	refAckThenFail refuser = "ack-then-fail"
)

// match counts a regexp's matches in a readback's stdout, the way tdc's
// matchPattern and matchCount do.
type match struct {
	Readback string `json:"readback"`
	Pattern  string `json:"pattern"`
	Count    int    `json:"count"`
}

// source says where a generated row came from, and what of its origin the
// row does not check.
type source struct {
	Tdc       string `json:"tdc,omitempty"`
	ID        string `json:"id,omitempty"`
	Unchecked string `json:"unchecked,omitempty"`
	// Selftest is a mined row's script line and function (kselftest.go).
	Selftest string `json:"selftest,omitempty"`
}

var (
	idRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.+-]*(/[a-z0-9][a-z0-9_.+-]*)*$`)
	nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
)

var descPrefixes = []string{"positive:", "negative:", "boundary:", "corner:"}

func (r row) validate() error {
	if !idRE.MatchString(r.ID) {
		return fmt.Errorf("bad id %q", r.ID)
	}
	if !hasDescPrefix(r.Desc) {
		return fmt.Errorf("%s: desc %q lacks a positive:/negative:/boundary:/corner: prefix", r.ID, r.Desc)
	}
	if strings.TrimSpace(r.Cmd) == "" {
		return fmt.Errorf("%s: no cmd", r.ID)
	}
	seen := map[string]bool{}
	for _, rb := range r.Readback {
		if !nameRE.MatchString(rb.Name) || rb.Name == "ip" || seen[rb.Name] {
			return fmt.Errorf("%s: bad or repeated readback name %q", r.ID, rb.Name)
		}
		if !validProto(rb.Proto) {
			return fmt.Errorf("%s: readback %s: proto %q", r.ID, rb.Name, rb.Proto)
		}
		seen[rb.Name] = true
	}
	if m := r.Expect.Match; m != nil && !seen[m.Readback] {
		return fmt.Errorf("%s: match reads readback %q, which the row lacks", r.ID, m.Readback)
	}
	if !validProto(r.Proto) {
		return fmt.Errorf("%s: proto %q", r.ID, r.Proto)
	}
	for _, m := range r.Modules {
		if !nameRE.MatchString(m) {
			return fmt.Errorf("%s: bad module %q", r.ID, m)
		}
	}
	return nil
}

// validProto is a protocol the workbench captures; "" is route.
func validProto(p string) bool {
	switch p {
	case "", "route", "generic", "xfrm", "sock_diag":
		return true
	}
	return false
}

func hasDescPrefix(desc string) bool {
	for _, p := range descPrefixes {
		if strings.HasPrefix(desc, p) {
			return true
		}
	}
	return false
}

// checkRows validates every row and refuses repeated ids.
func checkRows(rows []row) error {
	if len(rows) == 0 {
		return fmt.Errorf("no rows")
	}
	seen := map[string]int{}
	for i := range rows {
		r := &rows[i]
		if err := r.validate(); err != nil {
			return err
		}
		if j, ok := seen[r.ID]; ok {
			return fmt.Errorf("id %s repeats row %d", r.ID, j+1)
		}
		seen[r.ID] = i
	}
	return nil
}

func writeRows(w io.Writer, header []string, rows []row) error {
	if err := checkRows(rows); err != nil {
		return err
	}
	var b bytes.Buffer
	for _, h := range header {
		b.WriteString("# " + h + "\n")
	}
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for i := range rows {
		if err := enc.Encode(&rows[i]); err != nil {
			return err
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}

// skip is a source case with no row, and why.
type skip struct {
	Tdc    string `json:"tdc"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func writeSkips[T any](w io.Writer, header []string, skips []T) error {
	var b bytes.Buffer
	for _, h := range header {
		b.WriteString("# " + h + "\n")
	}
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	for _, s := range skips {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	_, err := w.Write(b.Bytes())
	return err
}
