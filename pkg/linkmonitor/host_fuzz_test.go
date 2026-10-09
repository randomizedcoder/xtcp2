package linkmonitor

import (
	"fmt"
	"regexp"
	"testing"
)

func FuzzHostPairs(f *testing.F) {
	for _, seed := range []string{"Tcp: MaxConn\nTcp: -1", "MPTcpExt:\nMPTcpExt:", "Tcp: A A\nTcp: 1 2", "Ip: X\nIp: 18446744073709551615"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumHostFile {
			return
		}
		p := hostParser{filter: regexp.MustCompile(".*")}
		p.reset()
		if err := p.paired(t.Context(), data); err == nil {
			if _, err := freezeSamples(nil, p.samples()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func FuzzHostIPv6(f *testing.F) {
	for _, seed := range []string{"Ip6InReceives 1", "Icmp6InType128 9007199254740993", "Ip6A 1\nIp6A 2", "Ip6 1"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maximumHostFile {
			return
		}
		p := hostParser{filter: regexp.MustCompile(".*")}
		p.reset()
		if err := p.ipv6(t.Context(), data); err == nil {
			if _, err := freezeSamples(nil, p.samples()); err != nil {
				t.Fatal(err)
			}
		}
	})
}

func BenchmarkHostCollection(b *testing.B) {
	for _, count := range []int{0, 64, 1024, 8192, maximumSamples} {
		for _, filter := range []string{".*", "^$"} {
			for _, cached := range []bool{false, true} {
				b.Run(fmt.Sprintf("fields=%d/filter=%s/cached=%t", count, filter, cached), func(b *testing.B) {
					files := map[string]string{"snmp": string(hostTestData(count)), "netstat": ""}
					c := hostTestCollector(b, files)
					c.parser.filter = regexp.MustCompile(filter)
					// Warm both alternating schema buffers before measuring reuse.
					for range 3 {
						if result := c.Collect(b.Context(), hostTestJob()); result.Err != nil {
							b.Fatal(result.Err)
						}
					}
					b.ReportAllocs()
					for b.Loop() {
						if !cached {
							clear(c.parser.cache)
						}
						if result := c.Collect(b.Context(), hostTestJob()); result.Err != nil {
							b.Fatal(result.Err)
						}
					}
				})
			}
		}
	}
}
