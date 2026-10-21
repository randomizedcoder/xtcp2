//go:build race

package main

// raceEnabled is true under -race, where sync.Pool drops a random quarter of
// what it is given, so allocation counts are not exact.
const raceEnabled = true
