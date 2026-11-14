//go:build goip_write

package goip

import (
	"reflect"
	"testing"
)

// TestWriteOpsTagged is TestWriteOpsUntagged's other half: with goip_write
// every field of writeOps is a write arm, none left the stub, so a field
// added to writeTable without its arm in init fails here.
func TestWriteOpsTagged(t *testing.T) {
	got, stub := reflect.ValueOf(writeOps), reflect.ValueOf(untaggedWriteOps())
	for i := range got.NumField() {
		f := got.Type().Field(i).Name
		if got.Field(i).IsNil() {
			t.Errorf("writeOps.%s is nil", f)
			continue
		}
		if got.Field(i).Pointer() == stub.Field(i).Pointer() {
			t.Errorf("writeOps.%s is still its stub", f)
		}
	}
}
