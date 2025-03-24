package main

import (
	"flag"
	"fmt"
	"log"
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

	sleep := flag.Duration("sleep", sleepDefaultDuration, "sleep duration")
	flag.Parse()

	for i := 0; i < initialNamespaces; i++ {
		createNamespace(namespaceName(i))
	}

	j := 0
	for i := 0; ; i++ {

		newNamespace := namespaceName(j + initialNamespaces)
		createNamespace(newNamespace)
		log.Printf("Added namespace: %s\n", newNamespace)

		oldestNamespace := namespaceName(j)
		removeNamespace(oldestNamespace)
		log.Printf("Removed namespace: %s\n", oldestNamespace)

		j++
		time.Sleep(*sleep)
	}
}

func namespaceName(index int) string {
	return fmt.Sprintf("%s%d", baseNamespaceName, index)
}

func createNamespace(name string) {

	log.Printf("createNamespace: ip netns add %s", name)
	cmd := exec.Command("ip", "netns", "add", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to create namespace %s: %v", name, err)
	}

}

func removeNamespace(name string) {
	log.Printf("removeNamespace: ip netns del %s", name)
	cmd := exec.Command("ip", "netns", "del", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to remove namespace %s: %v", name, err)
	}
}
