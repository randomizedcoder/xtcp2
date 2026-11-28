//go:build !gotc_write

package gotc

import "fmt"

// Without gotc_write gotc cannot lay out a write: every qdisc and class
// write verb is refused, as before increment 17 (qwrite.go is the tagged
// build's).

// qdiscWrite refuses do_qdisc's write verbs.
func (r *runner) qdiscWrite(verb string, _ []string) error {
	return fmt.Errorf("%w: tc qdisc %s", errNotImplemented, verb)
}

// classWrite refuses do_class's write verbs.
func (r *runner) classWrite(verb string, _ []string) error {
	return fmt.Errorf("%w: tc class %s", errNotImplemented, verb)
}

// writesBuilt is whether this build lays out qdisc and class writes.
const writesBuilt = false
