package goiphttp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip"
)

// The rules the index states, one per form.
const (
	commandRule = "ip [OPTIONS] OBJECT [VERB [ARGS...]] -> /ip/[OPTIONS/]OBJECT[/VERB[/ARGS...]]: " +
		"any read-only ip command line, one token per segment, abbreviated as ip abbreviates it; " +
		"/i is /ip; text unless -j"
	queryRule = "/ip/OBJECT[/INTERFACE][?PARAM=VALUE...]: a bare object, JSON unless format=text"
)

// example is one command line the index shows, with the full spelling of
// the command it is: the X-Goip-Argv a request for path carries.
// TestIndexExamples drives each through the handler.
type example struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	// NotServed names the goip gap that makes the path a 501 today. The
	// command is valid `ip`; the path starts working when goip parses it.
	NotServed string `json:"not_served,omitempty"`
}

// examples are the design's examples (IP-ARGV-PATHS-DESIGN.md section 1).
var examples = []example{
	{Path: "/ip/link/show", Command: "ip link show"},
	{Path: "/i/l/sh", Command: "ip link show"},
	{Path: "/i/a/s", Command: "ip address show"},
	{Path: "/ip/addr/show/dev/eth0", Command: "ip address show dev eth0"},
	{Path: "/ip/-br/a", Command: "ip -brief address show"},
	{Path: "/ip/-j/-s/link/show/dev/eth0", Command: "ip -stats -json link show dev eth0"},
	{Path: "/i/-4/r/s/table/all", Command: "ip -4 route show table all"},
	{Path: "/ip/addr/show/up/scope/global", Command: "ip address show up scope global"},
	{Path: "/ip/-d/link/show/type/vxlan", Command: "ip -details link show type vxlan"},
	{Path: "/i/-4/r/s/table/all/proto/bgp", Command: "ip -4 route show table all proto bgp"},
	{Path: "/ip/route/show/10.0.0.0/8", Command: "ip route show 10.0.0.0/8"},
	{Path: "/i/n/s/nud/reachable", Command: "ip neighbor show nud reachable"},
}

// indexEntry is one object in the /ip index.
type indexEntry struct {
	Path          string   `json:"path"`
	InterfacePath string   `json:"interface_path,omitempty"`
	Command       string   `json:"command"`
	Params        []string `json:"params"`
}

// indexDoc is the /ip index.
type indexDoc struct {
	Rule      string            `json:"rule"`
	QueryRule string            `json:"query_rule"`
	Objects   []indexEntry      `json:"objects"`
	Shortest  map[string]string `json:"shortest"`
	Examples  []example         `json:"examples"`
}

// indexDocument builds the index once, at New. Shortest is a map, which
// encoding/json writes in key order, so the document is the same bytes on
// every build.
func indexDocument() ([]byte, error) {
	doc := indexDoc{
		Rule:      commandRule,
		QueryRule: queryRule,
		Objects:   make([]indexEntry, 0, len(objects)),
		Shortest:  goip.Shortest(),
		Examples:  examples,
	}
	for _, o := range objects {
		e := indexEntry{
			Path:    Prefix + "/" + o.path,
			Command: "ip " + strings.Join(goip.ShowRequest{Object: o.object, JSON: true}.Argv(), " "),
			Params:  []string{formatParam},
		}
		if o.devPath {
			e.InterfacePath = e.Path + "/{interface}"
		}
		for _, p := range o.params {
			e.Params = append(e.Params, p.name)
		}
		doc.Objects = append(doc.Objects, e)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("goiphttp: index: %w", err)
	}
	return append(b, '\n'), nil
}
