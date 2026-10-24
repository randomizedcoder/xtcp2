package goiphttp

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// The command-line path's limits. A command line long enough to reach any
// of them is not one anybody types.
const (
	// argvTokensMax bounds the segments of a path, before the prefix join.
	argvTokensMax = 32
	// argvTokenMax bounds one decoded segment.
	argvTokenMax = 255
	// argvPathMax bounds the escaped path after the prefix.
	argvPathMax = 1024
)

// tokens splits escaped, the still-escaped path after /ip/, into the
// arguments of `ip`. Each segment is percent-decoded on its own, so a %2F
// stays inside its token, and a trailing / is ignored.
//
// A complete address followed by an all-digit segment is joined back into
// one ADDR/LEN token, so /ip/route/show/10.0.0.0/8 is `ip route show
// 10.0.0.0/8` and not `ip route show 10.0.0.0 8`, which ip reads as prefix 8.
// Both halves are kept as written, so ip's own parser still reads the
// length. Only a full address joins: a bare number may be a table id or a
// metric (`table 254 10`), and a zoned address is not a prefix.
//
// Every error wraps errParam: a 400, before any socket opens.
func tokens(escaped string) ([]string, error) {
	if len(escaped) > argvPathMax {
		return nil, paramErr("the path is longer than %d bytes", argvPathMax)
	}
	escaped = strings.TrimSuffix(escaped, "/")
	if escaped == "" {
		return nil, nil
	}
	n := strings.Count(escaped, "/") + 1
	if n > argvTokensMax {
		return nil, paramErr("the path has more than %d segments", argvTokensMax)
	}
	out := make([]string, 0, n)
	for seg, rest, more := escaped, "", true; more; seg = rest {
		seg, rest, more = strings.Cut(seg, "/")
		tok, err := url.PathUnescape(seg)
		if err != nil {
			return nil, paramErr("segment %d is not a valid escape", len(out)+1)
		}
		if err := validArgvToken(tok); err != nil {
			return nil, err
		}
		out = append(out, tok)
	}
	return joinPrefixes(out)
}

// validArgvToken applies the per-token limits: 1 to argvTokenMax bytes, no
// whitespace, no control characters and no invalid UTF-8.
func validArgvToken(tok string) error {
	if tok == "" {
		return paramErr("empty path segment")
	}
	if len(tok) > argvTokenMax {
		return paramErr("a path segment is longer than %d bytes", argvTokenMax)
	}
	for _, r := range tok {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == unicode.ReplacementChar {
			return paramErr("a path segment holds whitespace, a control character or invalid UTF-8")
		}
	}
	return nil
}

// joinPrefixes joins each full address and the all-digit token after it,
// in place. The length must fit the address's family: IPv4 0-32, IPv6
// (an IPv4-mapped one included) 0-128.
func joinPrefixes(toks []string) ([]string, error) {
	out := toks[:0]
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if i+1 < len(toks) && allDigits(toks[i+1]) {
			if addr, err := netip.ParseAddr(tok); err == nil && addr.Zone() == "" {
				family, bits := "IPv4", 32
				if addr.Is6() {
					family, bits = "IPv6", 128
				}
				if n, err := strconv.Atoi(toks[i+1]); err != nil || n > bits {
					return nil, paramErr("an %s prefix length is 0-%d", family, bits)
				}
				i++
				tok += "/" + toks[i]
			}
		}
		out = append(out, tok)
	}
	return out, nil
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
