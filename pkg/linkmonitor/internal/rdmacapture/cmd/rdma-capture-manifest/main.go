// Command rdma-capture-manifest finishes an RDMA capture directory written by
// nix/tests/rdma-native-capture.sh: it redacts every captured file in place
// and writes manifest.json. It also verifies existing capture directories.
//
//	rdma-capture-manifest write -dir STAGE -source l-mlx5 [-values FILE]
//	rdma-capture-manifest verify DIR...
//
// write consumes STAGE/index.tsv and STAGE/meta/ and removes them, so the
// directory that remains is exactly what the manifest lists. The values file
// holds the real identifiers and is never written into STAGE.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacapture"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: rdma-capture-manifest write -dir DIR -source NAME [-values FILE] | verify DIR...")
		return 2
	}
	var err error
	switch args[0] {
	case "write":
		err = write(args[1:], stdout)
	case "verify":
		err = verify(args[1:], stdout)
	default:
		err = fmt.Errorf("unknown subcommand %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(stderr, "rdma-capture-manifest:", err)
		return 1
	}
	return 0
}

func write(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("write", flag.ContinueOnError)
	dir := flags.String("dir", "", "capture directory")
	source := flags.String("source", "", "source name, such as l-mlx5 or vm-rxe")
	valuesFile := flags.String("values", "", "real identifiers to redact, one \"<kind> <value>\" per line")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *source == "" || flags.NArg() != 0 {
		return errors.New("write needs -dir and -source and no other arguments")
	}
	var values []rdmacapture.Value
	if *valuesFile != "" {
		f, err := os.Open(*valuesFile)
		if err != nil {
			return err
		}
		values, err = rdmacapture.ReadValues(f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	r, err := rdmacapture.NewRedactor(values)
	if err != nil {
		return err
	}
	index := map[string]rdmacapture.IndexEntry{}
	indexPath := filepath.Join(*dir, "index.tsv")
	if data, err := os.ReadFile(indexPath); err == nil { // #nosec G304 -- inside the capture directory
		if index, err = rdmacapture.ReadIndex(data); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	base := rdmacapture.Manifest{Source: *source}
	metaDir := filepath.Join(*dir, "meta")
	for name, field := range map[string]*string{
		"uname":     &base.Uname,
		"driver":    &base.Driver,
		"firmware":  &base.Firmware,
		"rdma_core": &base.RDMACore,
	} {
		data, err := os.ReadFile(filepath.Join(metaDir, name)) // #nosec G304 -- inside the capture directory
		if err != nil {
			return fmt.Errorf("meta: %w", err)
		}
		red, err := r.Redact(data, rdmacapture.FormatText)
		if err != nil {
			return err
		}
		*field = strings.TrimSpace(string(red))
	}
	if err := os.Remove(indexPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(metaDir); err != nil {
		return err
	}
	m, err := rdmacapture.Build(*dir, base, index, r)
	if err != nil {
		return err
	}
	if err := rdmacapture.Write(*dir, m); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d files, %d redacted values\n",
		filepath.Join(*dir, rdmacapture.ManifestName), len(m.Files), len(m.Redaction))
	return nil
}

func verify(dirs []string, stdout io.Writer) error {
	if len(dirs) == 0 {
		return errors.New("verify needs at least one directory")
	}
	var errs []error
	for _, dir := range dirs {
		m, err := rdmacapture.Verify(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		fmt.Fprintf(stdout, "ok %s: %d files\n", dir, len(m.Files))
	}
	return errors.Join(errs...)
}
