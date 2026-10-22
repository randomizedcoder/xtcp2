package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// tokenLinkBody is an ifinfomsg of family, index and flags, then attrs.
func tokenLinkBody(family uint8, index int32, flags uint32, attrs ...[]byte) []byte {
	b := make([]byte, IfInfomsgSizeCst)
	b[0] = family
	binary.LittleEndian.PutUint32(b[4:8], uint32(index))
	binary.LittleEndian.PutUint32(b[8:12], flags)
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

// tokenRta is one attribute, padded to 4 bytes; nested attrs concatenate.
func tokenRta(typ uint16, val ...[]byte) []byte {
	var p []byte
	for _, v := range val {
		p = append(p, v...)
	}
	return mcastRta(typ, p)
}

// TestParseLinkToken drives ParseLinkToken on constructed bodies.
//
// go test ./pkg/xtcpnl/ -run TestParseLinkToken
func TestParseLinkToken(t *testing.T) {
	tok := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 2, 0, 3, 0, 4}
	zero := make([]byte, 16)
	protinfo := func(attrs ...[]byte) []byte { return tokenRta(unix.IFLA_PROTINFO, attrs...) }
	flags6 := tokenRta(unix.IFLA_INET6_FLAGS, []byte{0, 0, 0, 0x80})
	tests := []struct {
		description string
		body        []byte
		expected    string
		err         error
	}{
		{"positive: the token after IFLA_INET6_FLAGS, as the kernel orders them",
			tokenLinkBody(unix.AF_INET6, 4, unix.IFF_UP, tokenRta(unix.IFLA_IFNAME, []byte("d0\x00")),
				protinfo(flags6, tokenRta(unix.IFLA_INET6_TOKEN, tok))),
			"fam 10 idx 4 flags 0x1 protinfo true token 00000000000000000001000200030004", nil},
		{"boundary: the all-zero token, `::`, is present", tokenLinkBody(unix.AF_INET6, 2, 0,
			protinfo(tokenRta(unix.IFLA_INET6_TOKEN, zero))),
			"fam 10 idx 2 flags 0x0 protinfo true token 00000000000000000000000000000000", nil},
		{"negative: IFLA_PROTINFO with no token", tokenLinkBody(unix.AF_INET6, 2, 0, protinfo(flags6)),
			"fam 10 idx 2 flags 0x0 protinfo true token nil", nil},
		{"negative: no IFLA_PROTINFO", tokenLinkBody(unix.AF_INET6, 2, 0, tokenRta(unix.IFLA_MTU, []byte{0, 5, 0, 0})),
			"fam 10 idx 2 flags 0x0 protinfo false token nil", nil},
		{"corner: an AF_BRIDGE reply's type 7 in IFLA_PROTINFO is not a token",
			tokenLinkBody(unix.AF_BRIDGE, 5, 0, protinfo(tokenRta(unix.IFLA_INET6_TOKEN, tok))),
			"fam 7 idx 5 flags 0x0 protinfo true token nil", nil},
		{"corner: the flags and index pass through for print_token's skips",
			tokenLinkBody(unix.AF_INET6, 1, unix.IFF_LOOPBACK|unix.IFF_NOARP, protinfo(tokenRta(unix.IFLA_INET6_TOKEN, zero))),
			"fam 10 idx 1 flags 0x88 protinfo true token 00000000000000000000000000000000", nil},
		{"corner: a repeated token takes the first", tokenLinkBody(unix.AF_INET6, 2, 0,
			protinfo(tokenRta(unix.IFLA_INET6_TOKEN, tok), tokenRta(unix.IFLA_INET6_TOKEN, zero))),
			"fam 10 idx 2 flags 0x0 protinfo true token 00000000000000000001000200030004", nil},
		{"corner: a repeated IFLA_PROTINFO takes the first", tokenLinkBody(unix.AF_INET6, 2, 0,
			protinfo(flags6), protinfo(tokenRta(unix.IFLA_INET6_TOKEN, tok))),
			"fam 10 idx 2 flags 0x0 protinfo true token nil", nil},
		{"corner: a malformed tail in the nest keeps the token before it", tokenLinkBody(unix.AF_INET6, 2, 0,
			protinfo(tokenRta(unix.IFLA_INET6_TOKEN, tok), []byte{0xff, 0, 7, 0})),
			"fam 10 idx 2 flags 0x0 protinfo true token 00000000000000000001000200030004", nil},
		{"boundary: a bare ifinfomsg", tokenLinkBody(unix.AF_INET6, 3, 0), "fam 10 idx 3 flags 0x0 protinfo false token nil", nil},
		{"negative: one byte short of an ifinfomsg", make([]byte, IfInfomsgSizeCst-1), "", ErrIfInfomsgSmall},
		{"negative: a malformed top-level attribute", tokenLinkBody(unix.AF_INET6, 2, 0, []byte{0xff, 0, 1, 0}), "", errAny},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			lt, err := ParseLinkToken(tc.body)
			switch {
			case tc.err == errAny && err == nil, tc.err != errAny && !errors.Is(err, tc.err):
				t.Fatalf("err = %v, want %v", err, tc.err)
			case err != nil:
				return
			}
			tk := "nil"
			if lt.Token != nil {
				tk = fmt.Sprintf("%x", lt.Token)
			}
			got := fmt.Sprintf("fam %d idx %d flags %#x protinfo %v token %s", lt.Family, lt.Index, lt.Flags, lt.HasProtinfo, tk)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// errAny stands for any error in a table row.
var errAny = errors.New("any error")

// TestParseLinkTokenCorpus decodes the AF_INET6 link dump `ip token show`
// sent in the reads corpus: every reply, and the one link with a token, v0's
// ::1:2:3:4. The loopback and the NOARP tunnels are in the dump, and
// print_token skips them, not the parser.
//
// go test ./pkg/xtcpnl/ -run TestParseLinkTokenCorpus
func TestParseLinkTokenCorpus(t *testing.T) {
	var replies, withToken, nonZero int
	var tokened []string
	for _, c := range corpusAll(t) {
		m := c.m
		if filepath.Base(c.path) != "netlink_route_getlink_token.pcap" || m.proto != unix.NETLINK_ROUTE ||
			m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 || len(m.body) == 0 ||
			m.body[0] != unix.AF_INET6 {
			continue
		}
		lt, err := ParseLinkToken(m.body)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		replies++
		if lt.Token != nil {
			withToken++
		}
		if lt.Token != nil && strings.Trim(fmt.Sprintf("%x", lt.Token), "0") != "" {
			nonZero++
			tokened = append(tokened, fmt.Sprintf("idx %d %x", lt.Index, lt.Token))
		}
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{"positive: sixteen AF_INET6 replies", fmt.Sprint(replies), "16"},
		{"positive: every one carries a token", fmt.Sprint(withToken), "16"},
		{"positive: one is not ::, v0's", fmt.Sprint(nonZero, tokened), "1 [idx 11 00000000000000000001000200030004]"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %q, expected %q", tc.got, tc.expected)
			}
		})
	}
}
