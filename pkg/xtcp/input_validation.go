package xtcp

import (
	"log"
	"strings"
)

func (x *XTCP) InputValidation() {

	if _, ok := x.Marshallers.Load(x.config.MarshalTo); !ok {
		log.Fatalf("InputValidation XTCP Marshal must be one of:%s MarshalTo:%s", validMarshallers(), x.config.MarshalTo)
	}

	if x.config.Dest != "null" {

		scheme, _, found := strings.Cut(x.config.Dest, ":")

		if !found {
			log.Fatalf("InputValidation XTCP Dest must contain ':' chars:%s", x.config.Dest)
		}

		if strings.Count(x.config.Dest, ":") != 2 {
			log.Fatalf("InputValidation XTCP Dest must contain x2 ':' chars:%s", x.config.Dest)
		}

		if _, status := lookupDestinationFactory(scheme); status != destLookupFound {
			log.Fatalf("InputValidation: %v", destinationLookupError(scheme, status))
		}
	}

	if len(x.config.Topic) < 1 || len(x.config.Topic) > 80 {
		log.Fatalf("InputValidation XTCP Topic must not be length < 1 or > 80:%d", len(x.config.Topic))
	}
}
