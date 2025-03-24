package io_uring

import "testing"

func TestSerialization(t *testing.T) {
	tests := []struct {
		name     string
		request  EncodedRequest
		expected uint64
	}{
		{
			name: "Test1",
			request: EncodedRequest{
				Operation: 0,
				NsID:      1,
				RequestID: 100,
			},
			expected: 0x0000010000000064,
		},
		{
			name: "Test2",
			request: EncodedRequest{
				Operation: 1,
				NsID:      65535,
				RequestID: 4294967295,
			},
			//expected: 0x01ffffffffffffffff,
			expected: 0x0000010000000064,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test serialization
			got := serialize(&tt.request)
			if got != tt.expected {
				t.Errorf("serialize() = 0x%016x, want 0x%016x", got, tt.expected)
			}

			// Test deserialization
			gotRequest := deserialize(got)
			if gotRequest != tt.request {
				t.Errorf("deserialize() = %+v, want %+v", gotRequest, tt.request)
			}
		})
	}
}
