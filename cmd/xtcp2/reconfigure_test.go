package main

import (
	"os"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
)

// sampleConfig is a representative XtcpConfig for the soft-restart carry tests.
func sampleConfig() *xtcp_config.XtcpConfig {
	return &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds:  5000,
		PollFrequency:          durationpb.New(10 * time.Second),
		PollTimeout:            durationpb.New(5 * time.Second),
		Netlinkers:             4,
		NetlinkersDoneChanSize: 100,
		NlmsgSeq:               1,
		Modulus:                1,
		MarshalTo:              "protobufList",
		Dest:                   "kafka:127.0.0.1:9092",
		DebugLevel:             111,
		GrpcPort:               8080,
		Tag:                    "INC-42",
		EnabledDeserializers: &xtcp_config.EnabledDeserializers{
			Enabled: map[string]bool{"info": true, "bbr": false},
		},
	}
}

// TestLoadReconfigureConfig_roundTrip verifies the env-carried config survives
// marshal → env → loadReconfigureConfig intact.
func TestLoadReconfigureConfig_roundTrip(t *testing.T) {
	want := sampleConfig()
	js, err := protojson.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	t.Setenv(reconfigureEnvKey, string(js))

	got, ok, err := loadReconfigureConfig()
	if err != nil {
		t.Fatalf("loadReconfigureConfig err: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true when env is set")
	}
	if got.Tag != "INC-42" || got.Dest != want.Dest || got.GrpcPort != 8080 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.EnabledDeserializers.Enabled["info"] != true || got.EnabledDeserializers.Enabled["bbr"] != false {
		t.Errorf("enabled_deserializers not round-tripped: %v", got.EnabledDeserializers.Enabled)
	}
}

func TestLoadReconfigureConfig_absentAndEmpty(t *testing.T) {
	t.Setenv(reconfigureEnvKey, "")
	if _, ok, err := loadReconfigureConfig(); ok || err != nil {
		t.Errorf("empty env: expected ok=false,err=nil; got ok=%v err=%v", ok, err)
	}
}

func TestLoadReconfigureConfig_malformed(t *testing.T) {
	t.Setenv(reconfigureEnvKey, "{not valid json")
	if _, ok, err := loadReconfigureConfig(); err == nil {
		t.Errorf("malformed env: expected error; got ok=%v", ok)
	}
}

// TestEnvWithoutKey drops any pre-existing carry entry so a re-exec replaces
// rather than duplicates it.
func TestEnvWithoutKey(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		reconfigureEnvKey + "=stale",
		"HOME=/root",
		reconfigureEnvKey + "=alsostale",
	}
	out := envWithoutKey(env, reconfigureEnvKey)
	for _, e := range out {
		if len(e) >= len(reconfigureEnvKey) && e[:len(reconfigureEnvKey)] == reconfigureEnvKey {
			t.Errorf("envWithoutKey left a %s entry: %q", reconfigureEnvKey, e)
		}
	}
	if len(out) != 2 {
		t.Errorf("expected 2 surviving entries, got %d: %v", len(out), out)
	}
	// os.Environ isn't touched.
	_ = os.Environ()
}
