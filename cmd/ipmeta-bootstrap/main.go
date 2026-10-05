// Command ipmeta-bootstrap builds a compact zstd-compressed lookup artifact
// from an ipfeed-collector Parquet artifact.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ipmeta-bootstrap:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ipmeta-bootstrap", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	in := fs.String("in", "", "input ipfeed collector Parquet artifact (.parquet or .parquet.zst)")
	out := fs.String("out", "", "output compact lookup artifact (.parquet.zst)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" || *out == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: ipmeta-bootstrap -in <artifact.parquet[.zst]> -out <bootstrap.lookup.parquet.zst>")
	}

	artifact, err := ipasn.LoadArtifact(*in)
	if err != nil {
		return fmt.Errorf("load input: %w", err)
	}
	stats, err := ipasn.PublishLookupCache(*out, "", artifact, ipasn.Stats{})
	if err != nil {
		return fmt.Errorf("write bootstrap: %w", err)
	}
	fmt.Printf("wrote %s prefixes=%d compressed_bytes=%d decompressed_bytes=%d\n",
		*out, stats.Prefixes, stats.CompressedBytes, stats.DecompressedBytes)
	return nil
}
