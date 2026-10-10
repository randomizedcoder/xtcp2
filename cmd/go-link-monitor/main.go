// go-link-monitor monitors physical network links and exports cached metrics.
package main

import "os"

var (
	version = "development"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(command(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
