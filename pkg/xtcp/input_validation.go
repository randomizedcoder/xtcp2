package xtcp

import (
	"log"
	"strings"
)

func (x *XTCP) InputValidation() {

	if *x.config.Modulus < 0 {
		log.Fatalf("InputValidation XTCP ReportModulus must not be < 0:%d", *x.config.Modulus)
	}

	if _, ok := x.Marshalers.Load(*x.config.Marshal); !ok {
		log.Fatalf("InputValidation XTCP Marshal must be one of proto, protojson, or prototext:%s", *x.config.Marshal)
	}

	dest, _, found := strings.Cut(*x.config.Dest, ":")
	if !found {
		log.Fatalf("InputValidation XTCP Dest must contain ':' chars:%s", *x.config.Dest)
	}

	if strings.Count(*x.config.Dest, ":") != 2 {
		log.Fatalf("InputValidation XTCP Dest must contain x2 ':' chars:%s", *x.config.Dest)
	}

	if _, ok := x.Destations.Load(dest); !ok {
		log.Fatalf("InputValidation XTCP Dest must start with kafka, udp, or nsq:%s", *x.config.Dest)
	}

	if len(*x.config.Topic) < 1 || len(*x.config.Topic) > 80 {
		log.Fatalf("InputValidation XTCP Topic must not be length < 1 or > 80:%d", len(*x.config.Topic))
	}
}
