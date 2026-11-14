package goip

import "github.com/randomizedcoder/xtcp2/internal/goip/render"

// getprotobyname and getprotobynumber, for the words and selectors that
// name an upper protocol: fou's and rule's ipproto, and an XFRM selector's
// UPSPEC. ip reads /etc/protocols; goip compiles in the parity guest's,
// NixOS's iana-etc (render/names.go), so a protocol a host adds is unknown
// and refused, or printed as its number, as ip would without that line.

// protoByName is getprotobyname.
func protoByName(name string) (uint8, bool) { return render.ProtoByName(name) }

// protoByNumber is getprotobynumber's p_name.
func protoByNumber(proto uint8) (string, bool) { return render.ProtoByNumber(proto) }
