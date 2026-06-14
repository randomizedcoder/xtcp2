package xtcp

import (
	"log"
	"strings"
)

func (x *XTCP) InputValidation() {

	if _, ok := x.Marshallers.Load(x.config.MarshalTo); !ok {
		log.Fatalf("InputValidation XTCP Marshal must be one of:%s MarshalTo:%s", validMarshallers(), x.config.MarshalTo)
	}

	if x.config.Dest != schemeNull {

		scheme, _, found := strings.Cut(x.config.Dest, ":")

		if !found {
			log.Fatalf("InputValidation XTCP Dest must contain ':' chars:%s", x.config.Dest)
		}

		// Schemes that take a network address (host:port) need exactly two
		// colons: `<scheme>:<host>:<port>`. Schemes that take a filesystem
		// path (unix/unixgram) need only one — the rest of the dest is a
		// path that can itself contain colons in pathological cases.
		switch scheme {
		case schemeUnix, schemeUnixgram:
			// only the leading `<scheme>:` separator is required; the
			// per-destination factory validates the path further.
		default:
			if strings.Count(x.config.Dest, ":") != 2 {
				log.Fatalf("InputValidation XTCP Dest must contain x2 ':' chars:%s", x.config.Dest)
			}
		}

		if _, status := lookupDestinationFactory(scheme); status != destLookupFound {
			log.Fatalf("InputValidation: %v", destinationLookupError(scheme, status))
		}
	}

	if len(x.config.Topic) < 1 || len(x.config.Topic) > 80 {
		log.Fatalf("InputValidation XTCP Topic must not be length < 1 or > 80:%d", len(x.config.Topic))
	}
}
