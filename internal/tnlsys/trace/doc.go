// Package trace runs a command under ptrace and records the tunnel ioctls
// it makes, as tnlsys snapshots. It is apart from tnlsys so that importers
// of tnlsys (goip, and through it the daemons) do not link os/exec; only
// cmd/tnlsnap imports it.
package trace
