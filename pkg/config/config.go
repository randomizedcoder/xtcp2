// Package xtcpConfig is the struct for the xtcpConfig that gets passed to most of the xtcp go routines
package config

import "time"

type Config struct {
	NLTimeout        *int64
	PollingFrequency *time.Duration
	MaxLoops         *int
	Netlinkers       *int
	NlmsgSeq         *int
	PacketSize       *int
	PacketSizeMply   *int
	WriteFiles       *int
	CapturePath      *string
	Modulus          *int
	Marshal          *string
	Dest             *string
	Topic            *string
	GoMaxProcs       *int
	PromListen       *string
	PromPath         *string
	DebugLevel       *int
}
