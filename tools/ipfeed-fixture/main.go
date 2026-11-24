// Command ipfeed-fixture crafts collector-shaped ASN lookup artifacts
// (Parquet, optionally zstd-wrapped) plus decompression "bombs" for the
// red-team harness (docs/redteam-plan.md rows rt-7-1..3: the artifact
// poisoner premise A5 — "controls an artifact the daemon ingests").
//
// It writes exactly the schema pkg/ipasn reads (prefix/asn/network_owner),
// so the crafted files are indistinguishable from real ipfeed-collector
// output to the loader — that is the point: the poisoning paths must be
// measured against artifacts the daemon cannot tell apart from legitimate
// ones by inspection of the shape.
//
// Modes:
//
//	rows  — a real Parquet artifact (rt-7-2 same-name swap, rt-7-3
//	        adversarial prefix tables: duplicate /32s, covering ladders,
//	        interleaved garbage prefixes the loader must skip).
//	bomb  — a .parquet.zst whose zstd stream decompresses to -decompressed
//	        bytes of filler (rt-7-1: the decompression is the attack; the
//	        content is garbage, so a loader without a cap dies reading, a
//	        loader with a cap refuses at the cap).
//
// Output naming convention: a path ending in .zst wraps the Parquet bytes in
// a zstd stream (SourceKind "parquet.zst" in ipasn.Stats); any other suffix
// writes the raw Parquet file ("parquet").
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/parquet-go/parquet-go"
)

// rowSchema mirrors pkg/ipasn's artifact schema: same column names, same
// types. The loader reads prefix as a string and parses it with
// netip.ParsePrefix, so invalid rows are skipped rather than fatal (the
// -invalid-every flag exercises that skip path).
type rowSchema struct {
	Prefix       string `parquet:"prefix"`
	ASN          uint32 `parquet:"asn"`
	NetworkOwner string `parquet:"network_owner"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ipfeed-fixture:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ipfeed-fixture", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "rows", "rows: craft a Parquet artifact; bomb: craft a zstd stream that decompresses to -decompressed bytes")
	n := fs.Int("n", 1000, "rows mode: number of rows")
	shape := fs.String("shape", "dup32", "rows mode: dup32 (all rows the same /32), ladder (decreasing prefix lengths, adversarial nesting), random24 (seeded /24s)")
	asn := fs.Uint("asn", 64512, "rows mode: the ASN every crafted row carries")
	owner := fs.String("owner", "ATTACKER-NET", "rows mode: the network_owner every crafted row carries")
	invalidEvery := fs.Int("invalid-every", 0, "rows mode: every Nth row gets a garbage prefix the loader must skip (0 = none)")
	prefix := fs.String("prefix", "203.0.113.7/32", "rows mode, shape=dup32: every row carries exactly this prefix (any valid CIDR — rt-7-2 poisons a whole range with one row repeated)")
	seed := fs.Int64("seed", 1, "rows mode: the deterministic seed for shape=random24")
	decompressed := fs.String("decompressed", "", "bomb mode: target decompressed size, e.g. 10GiB / 240MiB / 512MiB (Go-style suffixes)")
	out := fs.String("out", "", "output path; a .zst suffix wraps the payload in a zstd stream")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: ipfeed-fixture -mode rows|bomb -out <file.parquet[.zst]>")
	}

	switch *mode {
	case "rows":
		if *n <= 0 {
			return fmt.Errorf("-n must be > 0")
		}
		return writeRows(*out, *n, *shape, uint32(*asn), *owner, *invalidEvery, *prefix, *seed)
	case "bomb":
		if *decompressed == "" {
			return fmt.Errorf("bomb mode needs -decompressed (e.g. 10GiB)")
		}
		size, err := parseSize(*decompressed)
		if err != nil {
			return err
		}
		return writeBomb(*out, size)
	default:
		return fmt.Errorf("unknown mode %q (want rows|bomb)", *mode)
	}
}

// writeRows crafts the Parquet artifact. The prefix shapes:
//
//   - dup32: every row is the same host route (203.0.113.7/32). The loader
//     inserts it n times into the bart.Table — the duplicate-insert stress
//     (rt-7-3's "100k prefixes" in its most adversarial packing).
//   - ladder: rows walk every prefix length 0..32 over the same base IP and
//     then repeat the ladder with new bases — maximum nesting depth per
//     distinct prefix; the longest-prefix lookup always has a hit at some
//     depth.
//   - random24: seeded pseudo-random /24s — the "realistic table" baseline
//     for load-time comparison.
func writeRows(out string, n int, shape string, asn uint32, owner string, invalidEvery int, prefix string, seed int64) error {
	parsed, perr := netip.ParsePrefix(prefix)
	if perr != nil {
		return fmt.Errorf("-prefix %q: %w", prefix, perr)
	}
	rows := make([]rowSchema, 0, n)
	rng := rand.New(rand.NewSource(seed))
	for i := 0; i < n; i++ {
		if invalidEvery > 0 && i%invalidEvery == invalidEvery-1 {
			rows = append(rows, rowSchema{Prefix: "not-a-prefix", ASN: asn, NetworkOwner: owner})
			continue
		}
		var p netip.Prefix
		switch shape {
		case "dup32":
			p = parsed
		case "ladder":
			bits := i % 33
			p = netip.MustParsePrefix(fmt.Sprintf("203.0.%d.%d/%d", i/33, i/33, bits))
		case "random24":
			p = netip.MustParsePrefix(fmt.Sprintf("198.18.%d.%d/24", rng.Intn(256), rng.Intn(256)))
		default:
			return fmt.Errorf("unknown shape %q (want dup32|ladder|random24)", shape)
		}
		rows = append(rows, rowSchema{Prefix: p.String(), ASN: asn, NetworkOwner: owner})
	}

	var payload bytes.Buffer
	w := parquet.NewGenericWriter[rowSchema](&payload)
	if _, err := w.Write(rows); err != nil {
		return fmt.Errorf("parquet write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("parquet close: %w", err)
	}

	if err := emit(out, payload.Bytes()); err != nil {
		return err
	}
	fmt.Printf("wrote %s mode=rows shape=%s rows=%d asn=%d owner=%q\n",
		out, shape, n, asn, owner)
	return nil
}

// writeBomb crafts a .parquet.zst that decompresses to exactly size bytes of
// compressible filler. zstd turns the repeated block into ~1/1000 of its
// size, so a 10 GiB bomb is a ~10 MiB file — cheap to place, expensive to
// read. The content is not valid Parquet; the attack is the decompression
// itself (a capped loader refuses at its cap, an uncapped one allocates).
func writeBomb(out string, size int64) error {
	if size <= 0 {
		return fmt.Errorf("-decompressed must be > 0")
	}
	if !strings.HasSuffix(out, ".zst") {
		return fmt.Errorf("bomb mode writes a .zst stream; -out must end in .zst (got %q)", out)
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	bw := bufio.NewWriter(f)
	enc, err := zstd.NewWriter(bw)
	if err != nil {
		return err
	}
	block := bytes.Repeat([]byte("A"), 1<<20)
	var written int64
	for written < size {
		chunk := int64(len(block))
		if rem := size - written; rem < chunk {
			chunk = rem
		}
		if _, err := enc.Write(block[:chunk]); err != nil {
			return err
		}
		written += chunk
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s mode=bomb decompressed=%s compressed=%s ratio=%.0fx\n",
		out, human(size), human(fi.Size()), float64(size)/float64(fi.Size()))
	return nil
}

// emit writes the payload, wrapped in a zstd stream when the path ends in
// .zst — mirroring the two SourceKinds pkg/ipasn loads.
func emit(out string, payload []byte) error {
	if strings.HasSuffix(out, ".zst") {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		bw := bufio.NewWriter(f)
		enc, err := zstd.NewWriter(bw)
		if err != nil {
			return err
		}
		if _, err := enc.Write(payload); err != nil {
			return err
		}
		if err := enc.Close(); err != nil {
			return err
		}
		return bw.Flush()
	}
	return os.WriteFile(out, payload, 0o644)
}

// parseSize accepts the sizes the harness scripts quote: a bare integer or
// one of B/KiB/MiB/GiB (case-insensitive; K/M/G alone also accepted).
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	mult := int64(1)
	for k, m := range map[string]int64{
		"gib": 1 << 30, "mib": 1 << 20, "kib": 1 << 10,
		"gb": 1 << 30, "mb": 1 << 20, "kb": 1 << 10,
		"g": 1 << 30, "m": 1 << 20, "k": 1 << 10,
	} {
		if rest, ok := strings.CutSuffix(strings.ToLower(s), k); ok {
			s = rest
			mult = m
			break
		}
	}
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("-decompressed %q: %w", s, err)
	}
	return v * mult, nil
}

func human(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fGiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKiB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
