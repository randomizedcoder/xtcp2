// Package goiphttp serves goip's read-only `ip … show` output over HTTP, so an
// operator can `curl host/ip/addr | jq` instead of ssh'ing in to run `ip -j
// addr show`.
//
// Every path maps onto one goip.ShowRequest, and goip.Show is read-only by
// construction: the verb is always `show` and the object is matched exactly.
// This package adds the HTTP side of that: a strict query-parameter allowlist
// per object, a per-request netlink socket, an in-flight limit, a response
// size cap, buffering so an error never sends half a document, and a status
// code for each way a read can fail.
//
// The registry below is the single source for routing, validation and the
// /ip index. It is built once at package init; a request does map lookups
// against it and nothing else.
package goiphttp

import (
	"errors"
	"fmt"
	"strconv"
	"unicode"

	"github.com/randomizedcoder/xtcp2/internal/goip"
	"golang.org/x/sys/unix"
)

// Prefix is the path every route lives under.
const Prefix = "/ip"

// ifNameMax is IFNAMSIZ-1: the longest interface name the kernel accepts.
const ifNameMax = 15

// tokenMax bounds the stats group/subgroup/suite names, which are short
// keywords; nothing longer is a valid value.
const tokenMax = 32

// Words the registry uses more than once: object names that are also
// paths or parameter names, and the address family `link`.
const (
	linkWord  = "link"
	vrfWord   = "vrf"
	statsWord = "stats"
)

type paramKind uint8

const (
	// kindName is `KEY NAME`, an interface name (dev, master, vrf).
	kindName paramKind = iota
	// kindFlag is a bare selector keyword (proxy, groups, fdb), set by =1.
	kindFlag
	// kindTable is `table main|local|default|all|N`.
	kindTable
	// kindUint32 and kindUint8 are `KEY N` (nexthop id, protocol).
	kindUint32
	kindUint8
	// kindToken is `KEY WORD`, a lower-case keyword (stats group etc).
	kindToken
	// The options: they set ShowRequest fields rather than selectors.
	kindFamily
	kindStats
	kindDetails
)

// param is one accepted query parameter of one object.
type param struct {
	name string
	kind paramKind
	// exclusive means no other selector may accompany it (`nexthop show id
	// N` takes nothing after the id).
	exclusive bool
	// familyLink allows family=link (-0), which only `address show` has a
	// meaning for.
	familyLink bool
}

// object is one /ip/<path> route.
type object struct {
	path   string // the URL segment
	object string // goip's canonical object name
	// params in selector order: the order the selectors are emitted in, so
	// the argv is the same whatever order the query string used, and so
	// order-sensitive parsers (stats group before subgroup) are satisfied.
	params []param
	// byName indexes params; built at init.
	byName map[string]*param
	// devPath registers /ip/<path>/{interface} as `dev <interface>`.
	devPath bool
}

var (
	pDev     = param{name: "dev", kind: kindName}
	pFamily  = param{name: "family", kind: kindFamily}
	pStats   = param{name: statsWord, kind: kindStats}
	pDetails = param{name: "details", kind: kindDetails}
)

// objects is the registry. Paths use the short spelling an operator types
// (addr, neigh); the goip object is the canonical name.
var objects = []*object{
	{path: linkWord, object: linkWord, devPath: true,
		params: []param{pDev, pStats, pDetails}},
	{path: "addr", object: "address", devPath: true,
		params: []param{pDev, {name: "family", kind: kindFamily, familyLink: true}, pStats, pDetails}},
	{path: "route", object: "route", devPath: true,
		params: []param{{name: "table", kind: kindTable}, pDev, pFamily, pStats, pDetails}},
	{path: "neigh", object: "neighbor", devPath: true,
		params: []param{pDev, {name: "proxy", kind: kindFlag}, pFamily, pStats}},
	{path: "rule", object: "rule",
		params: []param{pFamily}},
	{path: "nexthop", object: "nexthop", devPath: true,
		params: []param{
			{name: "id", kind: kindUint32, exclusive: true},
			pDev,
			{name: "groups", kind: kindFlag},
			{name: "master", kind: kindName},
			{name: vrfWord, kind: kindName},
			{name: "protocol", kind: kindUint8},
			{name: "fdb", kind: kindFlag},
			pFamily,
		}},
	{path: vrfWord, object: vrfWord},
	{path: "netconf", object: "netconf", devPath: true,
		params: []param{pDev, pFamily}},
	{path: "ntable", object: "ntable",
		params: []param{pFamily, pStats}},
	{path: "addrlabel", object: "addrlabel"},
	{path: statsWord, object: statsWord, devPath: true,
		params: []param{
			pDev,
			{name: "group", kind: kindToken},
			{name: "subgroup", kind: kindToken},
			{name: "suite", kind: kindToken},
		}},
}

// formatParam is accepted on every path, including the index.
const formatParam = "format"

func init() {
	for _, o := range objects {
		o.byName = make(map[string]*param, len(o.params))
		for i := range o.params {
			o.byName[o.params[i].name] = &o.params[i]
		}
	}
}

// errParam is a request the registry refuses: always a 400.
var errParam = errors.New("bad request")

func paramErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errParam, fmt.Sprintf(format, a...))
}

// request is a validated request: what to show and how to encode it.
type request struct {
	show goip.ShowRequest
	text bool
}

// parse validates query (and the {interface} path value, "" when absent)
// against o and builds the ShowRequest. Unknown, repeated and inapplicable
// parameters are refused; nothing is silently ignored.
func (o *object) parse(query map[string][]string, pathDev string) (request, error) {
	r := request{show: goip.ShowRequest{Object: o.object, JSON: true}}
	values := make(map[string]string, len(query))
	for k, vs := range query {
		if len(vs) != 1 {
			return r, paramErr("parameter %q given %d times", k, len(vs))
		}
		if k == formatParam {
			switch vs[0] {
			case "json":
			case "text":
				r.text, r.show.JSON = true, false
			default:
				return r, paramErr("format %q: want json or text", vs[0])
			}
			continue
		}
		if _, ok := o.byName[k]; !ok {
			return r, paramErr("parameter %q is not accepted by %s/%s", k, Prefix, o.path)
		}
		values[k] = vs[0]
	}
	if pathDev != "" {
		if _, dup := values[pDev.name]; dup {
			return r, paramErr("dev given in both the path and the query")
		}
		values[pDev.name] = pathDev
	}

	selectors := 0
	for i := range o.params {
		p := &o.params[i]
		v, ok := values[p.name]
		if !ok {
			continue
		}
		if err := p.apply(&r.show, v); err != nil {
			return r, err
		}
		if p.isSelector() {
			selectors++
		}
	}
	for i := range o.params {
		p := &o.params[i]
		if _, ok := values[p.name]; ok && p.exclusive && selectors > 1 {
			return r, paramErr("%s cannot be combined with other selectors", p.name)
		}
	}
	return r, nil
}

func (p *param) isSelector() bool {
	switch p.kind {
	case kindFamily, kindStats, kindDetails:
		return false
	case kindName, kindFlag, kindTable, kindUint32, kindUint8, kindToken:
		return true
	}
	return true
}

// apply validates v for p and records it on s.
func (p *param) apply(s *goip.ShowRequest, v string) error {
	switch p.kind {
	case kindName:
		if !validIfName(v) {
			return paramErr("%s %q: not an interface name", p.name, v)
		}
		s.Selectors = append(s.Selectors, p.name, v)
	case kindFlag:
		if !truthy(v) {
			return paramErr("%s=%q: want 1 or true", p.name, v)
		}
		s.Selectors = append(s.Selectors, p.name)
	case kindTable:
		switch v {
		case "main", "local", "default", "all":
		default:
			n, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				return paramErr("table %q: want main, local, default, all or a number", v)
			}
			v = strconv.FormatUint(n, 10)
		}
		s.Selectors = append(s.Selectors, p.name, v)
	case kindUint32, kindUint8:
		bits := 32
		if p.kind == kindUint8 {
			bits = 8
		}
		n, err := strconv.ParseUint(v, 10, bits)
		if err != nil {
			return paramErr("%s %q: want a number below 2^%d", p.name, v, bits)
		}
		s.Selectors = append(s.Selectors, p.name, strconv.FormatUint(n, 10))
	case kindToken:
		if !validToken(v) {
			return paramErr("%s %q: want a lower-case keyword", p.name, v)
		}
		s.Selectors = append(s.Selectors, p.name, v)
	case kindFamily:
		switch {
		case v == "inet":
			s.Family = unix.AF_INET
		case v == "inet6":
			s.Family = unix.AF_INET6
		case v == linkWord && p.familyLink:
			s.Family = unix.AF_PACKET
		case p.familyLink:
			return paramErr("family %q: want inet, inet6 or link", v)
		default:
			return paramErr("family %q: want inet or inet6", v)
		}
	case kindStats:
		if !truthy(v) {
			return paramErr("stats=%q: want 1 or true", v)
		}
		s.Stats = true
	case kindDetails:
		if !truthy(v) {
			return paramErr("details=%q: want 1 or true", v)
		}
		s.Details = true
	}
	return nil
}

// truthy is the one spelling of "on" a boolean parameter accepts. `0` and
// the empty string are refused rather than read as off: an absent parameter
// is off, and a present one that says off is more likely a typo than intent.
func truthy(v string) bool { return v == "1" || v == "true" }

// validIfName accepts what the kernel's dev_valid_name accepts, minus the
// bytes that would make a header or a log line ambiguous: 1-15 bytes, not .
// or .., no /, no whitespace, no control characters.
func validIfName(v string) bool {
	if v == "" || len(v) > ifNameMax || v == "." || v == ".." {
		return false
	}
	for _, r := range v {
		if r == '/' || unicode.IsSpace(r) || unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}

func validToken(v string) bool {
	if v == "" || len(v) > tokenMax {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
