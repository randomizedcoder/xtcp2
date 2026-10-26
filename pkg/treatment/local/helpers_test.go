package local

import "net/netip"

func mustPrefix(s string) netip.Prefix { return netip.MustParsePrefix(s) }
