// Package dockermeta is a minimal Docker Engine API client that talks HTTP over
// the docker unix socket to build and maintain an index mapping a
// network-namespace inode -> the container that owns it.
//
// Why: xtcp2's TCP-socket records already carry the inode of the socket's
// owning network namespace. Docker reports, per container, a SandboxKey — the
// bind-mount path of that container's netns (e.g. /var/run/docker/netns/<hash>,
// visible to xtcp2 at /run/docker/netns). unix.Stat(SandboxKey).Ino yields the
// same nsfs inode the socket record carries, so keying an index on that inode
// lets the daemon label each record with container id/name/image.
//
// No moby SDK and no new module dependencies: just net/http over the unix
// socket, encoding/json, and golang.org/x/sys/unix (already required). The
// snapshot map is swapped atomically, mirroring pkg/cgroupid.
//
// This is best-effort enrichment: if the socket can't be dialed, New returns an
// error and the daemon treats enrichment as disabled. Containers on the host
// network (empty SandboxKey / inode 0) simply never match.
package dockermeta

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// DefaultSocketPath is the standard Docker Engine API unix socket.
const DefaultSocketPath = "/run/docker.sock"

// maxAPIVersion is the newest Docker Engine API version this client will speak.
// The actual version used is negotiated against the server (see New); this is
// only the ceiling. 1.44 works across a fleet whose MinAPIVersion ranges
// 1.40-1.44.
const maxAPIVersion = "1.44"

// relistInterval bounds how often the background refresh does a full re-list
// when the /events stream isn't available.
const relistInterval = 30 * time.Second

// eventsRetryDelay is the pause before reopening the /events stream (or falling
// back to a re-list) after it errors or closes.
const eventsRetryDelay = time.Second

// Container is the resolved identity of a container that owns a netns.
type Container struct {
	ID         string
	Name       string // leading slash stripped
	Image      string
	Pid        int
	SandboxKey string
	NetnsInode uint64 // unix.Stat(SandboxKey).Ino; 0 if unresolved
}

// statFn resolves a SandboxKey path to its inode. Production uses statInode
// (unix.Stat); tests inject a fake so no real filesystem is needed.
type statFn func(path string) (uint64, error)

// Index is a concurrency-safe, atomically-swapped snapshot of
// netnsInode -> Container, kept fresh by a background goroutine.
type Index struct {
	client  *http.Client
	baseURL string // e.g. "http://docker/v1.44" or "http://docker" (unversioned)
	stat    statFn

	snapshot atomic.Pointer[map[uint64]Container]

	cancel context.CancelFunc
	done   chan struct{}
}

// New dials the docker socket at socketPath (use DefaultSocketPath in
// production), negotiates the API version, builds an initial index, and starts
// a background goroutine that keeps it fresh. It returns an error if the
// initial dial/list fails; the caller treats that as "enrichment disabled".
func New(ctx context.Context, socketPath string) (*Index, error) {
	if socketPath == "" {
		socketPath = DefaultSocketPath
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	// Negotiate the API version up front so every request is versioned
	// consistently. A /version failure isn't fatal: Docker accepts unversioned
	// paths, so fall back to the bare host.
	base := "http://docker"
	if ver, err := negotiateVersion(ctx, client, base); err == nil && ver != "" {
		base = base + "/v" + ver
	}
	return newIndexFromClient(ctx, client, base, statInode)
}

// newIndexFromClient is the testable core: it takes a ready http.Client, a base
// URL already carrying any negotiated version prefix, and a statFn. It does the
// initial list+inspect (erroring if that fails) and launches the refresher.
func newIndexFromClient(ctx context.Context, client *http.Client, baseURL string, stat statFn) (*Index, error) {
	if stat == nil {
		stat = statInode
	}
	ix := &Index{
		client:  client,
		baseURL: baseURL,
		stat:    stat,
		done:    make(chan struct{}),
	}
	m, err := ix.build(ctx)
	if err != nil {
		return nil, err
	}
	ix.snapshot.Store(&m)

	runCtx, cancel := context.WithCancel(context.Background())
	ix.cancel = cancel
	go ix.refresh(runCtx) //nolint:contextcheck // the refresher outlives the caller's New() ctx by design; it is bounded by Index.Close()
	return ix, nil
}

// Lookup returns the container owning the given netns inode.
func (ix *Index) Lookup(netnsInode uint64) (Container, bool) {
	if m := ix.snapshot.Load(); m != nil {
		c, ok := (*m)[netnsInode]
		return c, ok
	}
	return Container{}, false
}

// Close stops the background refresh. It is safe to call once.
func (ix *Index) Close() error {
	if ix.cancel != nil {
		ix.cancel()
		<-ix.done
	}
	return nil
}

// versionInfo is the subset of GET /version we care about.
type versionInfo struct {
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
}

// negotiateVersion picks the API version to use: min(maxAPIVersion, server
// ApiVersion), but not below the server's MinAPIVersion. It returns "" (meaning
// "use unversioned paths") when /version can't be read.
func negotiateVersion(ctx context.Context, client *http.Client, base string) (string, error) {
	var vi versionInfo
	if err := getJSON(ctx, client, base+"/version", &vi); err != nil {
		return "", err
	}
	if vi.APIVersion == "" {
		return "", nil
	}
	chosen := vi.APIVersion
	if compareVersion(maxAPIVersion, chosen) < 0 {
		chosen = maxAPIVersion
	}
	if vi.MinAPIVersion != "" && compareVersion(chosen, vi.MinAPIVersion) < 0 {
		chosen = vi.MinAPIVersion
	}
	return chosen, nil
}

// compareVersion compares dotted numeric API versions ("1.44" vs "1.40"),
// returning -1, 0, or 1. Non-numeric or missing components sort low.
func compareVersion(a, b string) int {
	an := splitVersion(a)
	bn := splitVersion(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func splitVersion(s string) []int {
	var out []int
	cur := 0
	have := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			cur = cur*10 + int(c-'0')
			have = true
			continue
		}
		if c == '.' {
			out = append(out, cur)
			cur, have = 0, false
		}
	}
	if have || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// containerRef is one entry of GET /containers/json.
type containerRef struct {
	ID string `json:"Id"`
}

// inspect is the subset of GET /containers/{id}/json we decode.
type inspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"` // top-level fallback
	Config struct {
		Image string `json:"Image"`
	} `json:"Config"`
	State struct {
		Pid int `json:"Pid"`
	} `json:"State"`
	NetworkSettings struct {
		SandboxKey string `json:"SandboxKey"`
	} `json:"NetworkSettings"`
}

// build lists running containers, inspects each, and returns a fresh
// inode -> Container map. It errors only if the list request itself fails;
// per-container inspect failures are skipped so one bad container can't sink
// the whole index.
func (ix *Index) build(ctx context.Context) (map[uint64]Container, error) {
	var refs []containerRef
	if err := getJSON(ctx, ix.client, ix.baseURL+"/containers/json", &refs); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	m := make(map[uint64]Container, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if c, ok := ix.inspect(ctx, ref.ID); ok {
			m[c.NetnsInode] = c
		}
	}
	return m, nil
}

// inspect fetches and resolves a single container. It returns ok=false for
// containers we can't use: inspect error, malformed JSON, empty SandboxKey
// (host network), or an unresolvable inode.
func (ix *Index) inspect(ctx context.Context, id string) (Container, bool) {
	var in inspect
	if err := getJSON(ctx, ix.client, ix.baseURL+"/containers/"+id+"/json", &in); err != nil {
		return Container{}, false
	}
	if in.NetworkSettings.SandboxKey == "" {
		return Container{}, false // host-net container: nothing to key on
	}
	ino, err := ix.stat(in.NetworkSettings.SandboxKey)
	if err != nil || ino == 0 {
		return Container{}, false
	}
	image := in.Config.Image
	if image == "" {
		image = in.Image
	}
	name := in.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	return Container{
		ID:         in.ID,
		Name:       name,
		Image:      image,
		Pid:        in.State.Pid,
		SandboxKey: in.NetworkSettings.SandboxKey,
		NetnsInode: ino,
	}, true
}

// refresh keeps the snapshot fresh: it follows the /events stream and rebuilds
// on relevant container events, falling back to a periodic full re-list when
// the stream isn't available. It exits on ctx cancellation, closing done.
func (ix *Index) refresh(ctx context.Context) {
	defer close(ix.done)
	for {
		if err := ix.streamEvents(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			// Stream unavailable/closed: pause, then do a full re-list so we
			// don't go stale while events are down.
			select {
			case <-ctx.Done():
				return
			case <-time.After(eventsRetryDelay):
			}
			ix.rebuild(ctx)
			select {
			case <-ctx.Done():
				return
			case <-time.After(relistInterval):
			}
			continue
		}
		// streamEvents returned without error only on ctx cancellation.
		return
	}
}

// dockerEvent is the subset of a /events stream object we react to.
type dockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
}

// streamEvents opens GET /events filtered to container start/die/destroy and
// rebuilds the index on each. It returns nil only when ctx is canceled; any
// other outcome (dial error, EOF, decode error) returns a non-nil error so the
// caller can fall back to periodic re-listing.
func (ix *Index) streamEvents(ctx context.Context) error {
	const filters = `{"type":["container"],"event":["start","die","destroy"]}`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ix.baseURL+"/events?filters="+urlQueryEscape(filters), nil)
	if err != nil {
		return err
	}
	resp, err := ix.client.Do(req)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events: status %d", resp.StatusCode)
	}
	// The stream emits one JSON object per line and never ends on its own; we
	// rebuild on each relevant event.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev dockerEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // tolerate a malformed line
		}
		if ev.Type != "container" {
			continue
		}
		switch ev.Action {
		case "start", "die", "destroy":
			ix.rebuild(ctx)
		}
		if ctx.Err() != nil {
			return nil
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return io.EOF // stream closed by the server
}

// rebuild does a fresh build and atomically swaps it in. On error it keeps the
// previous snapshot rather than blanking the index.
func (ix *Index) rebuild(ctx context.Context) {
	m, err := ix.build(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("dockermeta: rebuild: %v", err)
		}
		return
	}
	ix.snapshot.Store(&m)
}

// getJSON does a GET and decodes the JSON body into v. A non-2xx status or a
// decode error is returned so callers can skip/fall back.
func getJSON(ctx context.Context, client *http.Client, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// drainClose drains and closes a response body so the underlying connection can
// be reused, then closes it.
func drainClose(rc io.ReadCloser) {
	io.Copy(io.Discard, rc) //nolint:errcheck,gosec // drain to enable connection reuse; body content is unused
	_ = rc.Close()
}

// urlQueryEscape percent-escapes a query-string value. Kept local and minimal
// to avoid importing net/url for one call.
func urlQueryEscape(s string) string {
	const hex = "0123456789ABCDEF"
	buf := make([]byte, 0, len(s)*3)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			buf = append(buf, c)
			continue
		}
		buf = append(buf, '%', hex[c>>4], hex[c&0xf])
	}
	return string(buf)
}

// statInode is the production statFn: it stats a SandboxKey path and returns
// its inode.
func statInode(path string) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, err
	}
	return st.Ino, nil
}
