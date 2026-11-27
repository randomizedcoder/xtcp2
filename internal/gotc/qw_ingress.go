//go:build gotc_write

package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// q_ingress.c and q_clsact.c parse_qopt at iproute2 v7.2.0. Neither sends
// TCA_OPTIONS.

// parseIngressQopt is ingress_parse_opt: `handle X` pairs are skipped
// unchecked (NEXT_ARG, then the pair's second word).
func parseIngressQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	for len(args) > 0 {
		if args[0] != "handle" {
			r.errf("What is \"%s\"?\n", args[0])
			r.errf("Usage: ... ingress\n")
			return nil, errKindParse
		}
		if len(args) < 2 {
			return nil, r.incomplete()
		}
		args = args[2:]
	}
	return nil, nil
}

// parseClsactQopt is clsact_parse_opt: any word is refused.
func parseClsactQopt(r *runner, args []string, _ string) (func(*xtcpnl.AttrBuilder) error, error) {
	if len(args) > 0 {
		r.errf("What is \"%s\"?\n", args[0])
		r.errf("Usage: ... clsact\n")
		return nil, errKindParse
	}
	return nil, nil
}
