package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

const (
	baseNamespaceName = "ns"
	initialNamespaces = 1000
	namespaceDir      = "/run/netns"

	sleepDefaultDuration = 100 * time.Millisecond
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stderr))
}

// runMain wires flag parsing + the churn loop. Extracted so tests can
// drive it with a cancellable ctx + synthetic args, without actually
// shelling out to `ip netns` for the full 1000-namespace initial fill.
func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("nsTest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sleep := fs.Duration("sleep", sleepDefaultDuration, "sleep duration")
	initialCount := fs.Int("initial", initialNamespaces, "initial namespace count (for tests; production keeps the 1000 default)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Initial-fill phase: create `*initialCount` namespaces.
	for i := 0; i < *initialCount; i++ {
		if ctx.Err() != nil {
			return 0
		}
		createNamespace(ctx, namespaceName(i))
	}

	// Churn loop: alternately create+remove one namespace per tick.
	return churn(ctx, *initialCount, *sleep)
}

// churn is the production-mode forever loop: add one namespace and
// remove the oldest each iteration, sleeping `sleep` between rounds.
// Returns 0 on ctx cancel.
func churn(ctx context.Context, initial int, sleep time.Duration) int {
	j := 0
	for {
		if ctx.Err() != nil {
			return 0
		}
		newNamespace := namespaceName(j + initial)
		createNamespace(ctx, newNamespace)
		log.Printf("Added namespace: %s\n", newNamespace)

		oldestNamespace := namespaceName(j)
		removeNamespace(ctx, oldestNamespace)
		log.Printf("Removed namespace: %s\n", oldestNamespace)

		j++
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(sleep):
		}
	}
}

func namespaceName(index int) string {
	return fmt.Sprintf("%s%d", baseNamespaceName, index)
}

func createNamespace(ctx context.Context, name string) {

	log.Printf("createNamespace: ip netns add %s", name)
	cmd := exec.CommandContext(ctx, "ip", "netns", "add", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to create namespace %s: %v", name, err)
	}

}

func removeNamespace(ctx context.Context, name string) {
	log.Printf("removeNamespace: ip netns del %s", name)
	cmd := exec.CommandContext(ctx, "ip", "netns", "del", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to remove namespace %s: %v", name, err)
	}
}
