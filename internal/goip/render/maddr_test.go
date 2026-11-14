package render

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestMaddr is print_mlist in text and under -j: the header on each new
// index, `users` only when not 1, `static` from dev_mcast.
//
// go test ./internal/goip/render/ -run TestMaddr
func TestMaddr(t *testing.T) {
	link := MaddrEntry{Index: 3, Name: "d0", Family: unix.AF_PACKET, Addr: []byte{0x33, 0x33, 0, 0, 0, 1}, Users: 1}
	inet := MaddrEntry{Index: 3, Name: "d0", Family: unix.AF_INET, Addr: []byte{239, 1, 1, 1}, Users: 1}
	inet6 := MaddrEntry{Index: 3, Name: "d0", Family: unix.AF_INET6,
		Addr: []byte{0xff, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, Users: 1}
	lo := MaddrEntry{Index: 1, Name: "lo", Family: unix.AF_INET, Addr: []byte{224, 0, 0, 1}, Users: 1}
	with := func(m MaddrEntry, users uint32, static bool) MaddrEntry { m.Users, m.Static = users, static; return m }
	tests := []struct {
		description string
		list        []MaddrEntry
		expected    string
		json        string
	}{
		{description: "corner: no groups prints nothing, and an empty array", expected: "", json: "[]\n"},
		{description: "positive: one device, each family's spelling", list: []MaddrEntry{link, inet, inet6},
			expected: "3:\td0\n\tlink  33:33:00:00:00:01\n\tinet  239.1.1.1\n\tinet6 ff02::1\n",
			json: `[{"ifindex":3,"ifname":"d0","maddr":[{"link":"33:33:00:00:00:01"},{"family":"inet","address":"239.1.1.1"},` +
				`{"family":"inet6","address":"ff02::1"}]}]` + "\n"},
		{description: "positive: a header for each index", list: []MaddrEntry{lo, inet},
			expected: "1:\tlo\n\tinet  224.0.0.1\n3:\td0\n\tinet  239.1.1.1\n",
			json: `[{"ifindex":1,"ifname":"lo","maddr":[{"family":"inet","address":"224.0.0.1"}]},` +
				`{"ifindex":3,"ifname":"d0","maddr":[{"family":"inet","address":"239.1.1.1"}]}]` + "\n"},
		{description: "positive: users other than 1, then static", list: []MaddrEntry{with(link, 2, true)},
			expected: "3:\td0\n\tlink  33:33:00:00:00:01 users 2 static\n",
			json:     `[{"ifindex":3,"ifname":"d0","maddr":[{"link":"33:33:00:00:00:01","users":2,"features":"static"}]}]` + "\n"},
		{description: "boundary: users 0 prints", list: []MaddrEntry{with(inet, 0, false)},
			expected: "3:\td0\n\tinet  239.1.1.1 users 0\n",
			json:     `[{"ifindex":3,"ifname":"d0","maddr":[{"family":"inet","address":"239.1.1.1","users":0}]}]` + "\n"},
		{description: "boundary: the largest users count", list: []MaddrEntry{with(inet, 4294967295, false)},
			expected: "3:\td0\n\tinet  239.1.1.1 users 4294967295\n",
			json:     `[{"ifindex":3,"ifname":"d0","maddr":[{"family":"inet","address":"239.1.1.1","users":4294967295}]}]` + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MaddrText(tc.list); got != tc.expected {
				t.Errorf("text %q, want %q", got, tc.expected)
			}
			b, err := MaddrJSON(tc.list)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.json {
				t.Errorf("json %s, want %s", b, tc.json)
			}
		})
	}
}
