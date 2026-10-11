package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorctl"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	ctlCommand = "ctl"
	ctlProgram = "go-link-monitor ctl"
	// defaultCtlTimeout covers the longest server-side wait (60s) plus slack.
	defaultCtlTimeout = 75 * time.Second
)

// ctlOptions are the client flags placed before the subcommand.
type ctlOptions struct {
	network, address string
	timeout          time.Duration
	read, admin      credentialOptions
}

// ctl runs `go-link-monitor ctl [flags] <command> [command flags]` against
// the admin listener. It sends the admin credential for admin commands when
// one is configured, and the read credential otherwise.
func ctl(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	o := ctlOptions{}
	flags := flag.NewFlagSet(ctlProgram, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&o.network, "network", listener.NetworkUnix, "Admin listener network: unix or tcp; overrides "+adminNetworkEnvironment)
	flags.StringVar(&o.address, "address", defaultAdminSocket, "Admin socket path or TCP address; overrides "+adminAddressEnvironment)
	flags.DurationVar(&o.timeout, "timeout", defaultCtlTimeout, "Overall deadline for the call (positive)")
	flags.StringVar(&o.read.tokenFile, "auth.read-token-file", "", "File holding the read raw bearer token; overrides "+readTokenEnvironment)
	flags.StringVar(&o.read.hmacKeyFile, "auth.read-hmac-key-file", "", "File holding the read HMAC shared key; overrides "+readHMACEnvironment)
	flags.StringVar(&o.admin.tokenFile, "auth.admin-token-file", "", "File holding the admin raw bearer token; overrides "+adminTokenEnvironment)
	flags.StringVar(&o.admin.hmacKeyFile, "auth.admin-hmac-key-file", "", "File holding the admin HMAC shared key; overrides "+adminHMACEnvironment)
	flags.Usage = func() {
		linkmonitorctl.Usage(stderr, ctlProgram+" [flags]")
		_, _ = fmt.Fprintln(stderr, "\nFlags:")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for _, fallback := range []envFallback{
		{"network", adminNetworkEnvironment, &o.network},
		{"address", adminAddressEnvironment, &o.address},
		{"auth.read-token-file", readTokenEnvironment, &o.read.tokenFile},
		{"auth.read-hmac-key-file", readHMACEnvironment, &o.read.hmacKeyFile},
		{"auth.admin-token-file", adminTokenEnvironment, &o.admin.tokenFile},
		{"auth.admin-hmac-key-file", adminHMACEnvironment, &o.admin.hmacKeyFile},
	} {
		if value := getenv(fallback.environment); !explicit[fallback.flag] && value != "" {
			*fallback.target = value
		}
	}
	target, err := o.target()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", ctlProgram, err)
		return 2
	}
	read, err := listenerauth.ClientAuthFromValues("", o.read.tokenFile, "", o.read.hmacKeyFile)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: read credential: %v\n", ctlProgram, err)
		return 2
	}
	admin, err := listenerauth.ClientAuthFromValues("", o.admin.tokenFile, "", o.admin.hmacKeyFile)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: admin credential: %v\n", ctlProgram, err)
		return 2
	}
	rest := flags.Args()
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", ctlProgram, err)
		return 1
	}
	defer func() {
		if err := conn.Close(); err != nil {
			_, _ = fmt.Fprintf(stderr, "%s: close: %v\n", ctlProgram, err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	ctx = listenerauth.ContextWithClientAuth(ctx, listenerauth.ForAccess(read, admin, linkmonitorctl.Access(rest)), time.Now())
	return linkmonitorctl.Run(ctx, conn, ctlProgram, rest, stdout, stderr)
}

// target validates the options and returns the gRPC dial target.
func (o ctlOptions) target() (string, error) {
	if o.timeout <= 0 {
		return "", fmt.Errorf("-timeout %s must be positive", o.timeout)
	}
	if o.address == "" {
		return "", errors.New("-address must not be empty")
	}
	switch o.network {
	case listener.NetworkUnix:
		return "unix://" + o.address, nil
	case listener.NetworkTCP:
		return "dns:///" + o.address, nil
	default:
		return "", fmt.Errorf("-network %q must be unix or tcp", o.network)
	}
}
