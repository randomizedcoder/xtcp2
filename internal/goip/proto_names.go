package goip

// getprotobyname and getprotobynumber, for the words and selectors that
// name an upper protocol. ip reads /etc/protocols; goip reads no config
// files, so it knows the names NixOS's iana-etc gives the protocols fou's
// and rule's ipproto take, which take in the protocols an XFRM selector's
// UPSPEC names. A name or a number outside them is the caller's to refuse
// as not implemented, where ip would look it up.

// protoByName is getprotobyname over goip's names.
func protoByName(name string) (uint8, bool) {
	if p, ok := fouProtoNames[name]; ok {
		return p, true
	}
	p, ok := ruleProtoNames[name]
	return p, ok
}

// protoByNumber is getprotobynumber over goip's names: the name iana-etc
// gives the number, which is each number's only one.
func protoByNumber(proto uint8) (string, bool) {
	for _, names := range []map[string]uint8{fouProtoNames, ruleProtoNames} {
		for name, p := range names {
			if p == proto {
				return name, true
			}
		}
	}
	return "", false
}
