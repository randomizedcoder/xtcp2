package xtcp

import (
	"fmt"
	"strings"
)

// InputValidation is the original log.Fatalf wrapper around validateInput.
// Kept for the existing call site in init.go; new code should call
// validateInput directly so the error path is testable. Routes through
// x.callFatalf which falls back to log.Fatalf when x.fatalf is nil.
func (x *XTCP) InputValidation() {
	if err := x.validateInput(); err != nil {
		x.callFatalf("InputValidation: %v", err)
	}
}

// validateInput checks XTCP's runtime configuration and returns a
// descriptive error rather than fataling. The wrapper above preserves
// the legacy log.Fatalf behavior for the init-time call site.
func (x *XTCP) validateInput() error {
	if _, ok := x.Marshallers.Load(x.config.MarshalTo); !ok {
		if _, ok := x.EnvelopeMarshallers.Load(x.config.MarshalTo); !ok {
			return fmt.Errorf("XTCP Marshal must be one of:%s MarshalTo:%s",
				validMarshallers(), x.config.MarshalTo)
		}
	}

	if x.config.Dest != schemeNull {

		scheme, _, found := strings.Cut(x.config.Dest, ":")
		if !found {
			return fmt.Errorf("XTCP Dest must contain ':' chars:%s", x.config.Dest)
		}

		// Schemes that take a network address (host:port) need exactly two
		// colons: `<scheme>:<host>:<port>`. Schemes that take a filesystem
		// path (unix/unixgram) need only one — the rest of the dest is a
		// path that can itself contain colons in pathological cases. The
		// null scheme takes no payload at all; `null` (bare) bypasses this
		// block via the schemeNull early-return above, but `null:` makes
		// it here. Treat both as path-style to keep `-dest null` and
		// `-dest null:` symmetric — the previous switch only listed
		// unix/unixgram, so `-dest null:` (with the documented
		// schemeNullPrefix colon) failed validation as "must contain x2
		// colons" while the registry happily had a "null" factory.
		switch scheme {
		case schemeUnix, schemeUnixgram, schemeNull:
			// only the leading `<scheme>:` separator is required; the
			// per-destination factory validates the path further.
		default:
			if strings.Count(x.config.Dest, ":") != 2 {
				return fmt.Errorf("XTCP Dest must contain x2 ':' chars:%s", x.config.Dest)
			}
		}

		if _, status := lookupDestinationFactory(scheme); status != destLookupFound {
			return destinationLookupError(scheme, status)
		}
	}

	if len(x.config.Topic) < 1 || len(x.config.Topic) > 80 {
		return fmt.Errorf("XTCP Topic must not be length < 1 or > 80:%d",
			len(x.config.Topic))
	}
	return nil
}
