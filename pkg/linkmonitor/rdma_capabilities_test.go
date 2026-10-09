package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacaps"
)

func TestRDMACapabilityPolicyTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		active                                       uint32
		width                                        uint8
		up                                           model.Optional[bool]
		complete                                     bool
		speedCheck, widthCheck                       model.Check
	}{
		{"maximum", "positive", "NDR 4x supported and active", "speed and width pass", 128, 2, presentValue(true), true, model.CheckPass, model.CheckPass},
		{"speed", "negative", "HDR enabled on NDR hardware", "speed fails against supported maximum", 64, 2, presentValue(true), true, model.CheckFail, model.CheckPass},
		{"width", "negative", "NDR 1x on 4x hardware", "both aggregate speed and width fail", 128, 1, presentValue(true), true, model.CheckFail, model.CheckFail},
		{"down", "corner", "down port", "negotiation not applicable", 0, 0, presentValue(false), true, model.CheckNotApplicable, model.CheckNotApplicable},
		{"unknown", "boundary", "future active speed", "speed unknown independently of width", 512, 2, presentValue(true), true, model.CheckUnknown, model.CheckPass},
		{"incomplete", "corner", "incomplete supported mask", "both maxima unknown", 128, 2, presentValue(true), false, model.CheckUnknown, model.CheckUnknown},
		{"state", "negative", "missing authoritative up state", "checks unknown", 128, 2, model.Optional[bool]{}, true, model.CheckUnknown, model.CheckUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			caps := rdmacaps.Capabilities{Supported: 128 | 64, Enabled: 64, Active: tc.active, WidthSupported: 2, WidthEnabled: 1, WidthActive: tc.width, Complete: tc.complete}
			samples, checks := rdmaCapabilitySamples(tc.up, model.RDMAPort{Device: "hca", Port: 1}, caps)
			if checks.Speed != tc.speedCheck || checks.Width != tc.widthCheck || checks.Duplex != model.CheckNotApplicable {
				t.Fatalf("%s: %+v", tc.expectedOutcome, checks)
			}
			for _, sample := range samples {
				if sample.Descriptor == interfaceDescriptors["max_speed_bits_per_second"] {
					if n, ok := sample.Number.Uint64(); !ok || n != 400000000000 {
						t.Fatal("enabled mask substituted for supported maximum")
					}
				}
			}
		})
	}
}
