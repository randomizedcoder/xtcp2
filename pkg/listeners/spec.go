package listeners

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/listener"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// SpecSyntax documents ParseSpec's format, for flag usage strings.
const SpecSyntax = "name=<name>,serves=<class>[+<class>...],address=<path|host:port>" +
	"[,network=unix|tcp][,mode=0600][,unlinkStale=true]" +
	"[,auth=raw|hmac-utc-minute|disabled][,tokenFile=<path>][,hmacKeyFile=<path>]" +
	"[,adminAuth=raw|hmac-utc-minute][,adminTokenFile=<path>][,adminHmacKeyFile=<path>]"

// ParseSpec parses one listener in the -listener / LISTENERS form, e.g.
//
//	name=metrics,serves=health+metrics,address=/run/xtcp2/metrics.sock,tokenFile=/run/secrets/metrics
//
// Credentials are files only: the config carries paths, never secrets.
// Unset auth inherits the daemon-wide credential. Field-level checks are
// left to protovalidate and Validate.
func ParseSpec(s string) (*xtcp_config.Listener, error) {
	l := &xtcp_config.Listener{Endpoint: &xtcp_config.ListenerEndpoint{UnlinkStaleUnixSocket: true}}
	seen := map[string]bool{}
	for _, kv := range strings.Split(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("%w: listener spec %q: %q is not key=value", ErrConfig, s, kv)
		}
		if seen[k] {
			return nil, fmt.Errorf("%w: listener spec %q: %s given twice", ErrConfig, s, k)
		}
		seen[k] = true
		if err := setField(l, k, strings.TrimSpace(v)); err != nil {
			return nil, fmt.Errorf("%w: listener spec %q: %w", ErrConfig, s, err)
		}
	}
	if l.GetName() == "" {
		return nil, fmt.Errorf("%w: listener spec %q: name= is required", ErrConfig, s)
	}
	if l.GetEndpoint().GetAddress() == "" {
		return nil, fmt.Errorf("%w: listener spec %q: address= is required", ErrConfig, s)
	}
	return l, nil
}

func setField(l *xtcp_config.Listener, k, v string) error {
	ep := l.Endpoint
	switch k {
	case "name":
		l.Name = v
	case "address":
		ep.Address = v
	case "network":
		n, err := listener.ParseNetwork(v)
		if err != nil {
			return err
		}
		ep.Network = n
	case "mode":
		m, err := strconv.ParseUint(v, 0, 32)
		if err != nil || m > 0o777 {
			return fmt.Errorf("mode %q: want an octal permission like 0600", v)
		}
		ep.UnixSocketMode = uint32(m)
	case "unlinkStale":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("unlinkStale %q: %w", v, err)
		}
		ep.UnlinkStaleUnixSocket = b
	case "serves":
		for _, n := range strings.Split(v, "+") {
			c, err := ParseClass(n)
			if err != nil {
				return err
			}
			l.Serves = append(l.Serves, c)
		}
	case "auth", "adminAuth":
		m, err := listenerauth.ParseMode(v)
		if err != nil {
			return err
		}
		authFor(l, k == "adminAuth").Mode = m
	case "tokenFile":
		authFor(l, false).RawTokenFile = v
	case "hmacKeyFile":
		authFor(l, false).HmacSharedKeyFile = v
	case "adminTokenFile":
		authFor(l, true).RawTokenFile = v
	case "adminHmacKeyFile":
		authFor(l, true).HmacSharedKeyFile = v
	default:
		return fmt.Errorf("unknown key %q; syntax: %s", k, SpecSyntax)
	}
	return nil
}

func authFor(l *xtcp_config.Listener, admin bool) *xtcp_config.ListenerAuth {
	if admin {
		if l.AdminAuth == nil {
			l.AdminAuth = &xtcp_config.ListenerAuth{}
		}
		return l.AdminAuth
	}
	if l.Auth == nil {
		l.Auth = &xtcp_config.ListenerAuth{}
	}
	return l.Auth
}

// FormatSpec is l in ParseSpec's form, for logs and the deprecation hint.
// Unset fields are left out; credentials are files, so nothing secret is
// printed.
func FormatSpec(l *xtcp_config.Listener) string {
	ep := l.GetEndpoint()
	cls := make([]string, 0, len(l.GetServes()))
	for _, c := range l.GetServes() {
		cls = append(cls, ClassName(c))
	}
	kv := []string{"name=" + l.GetName(), "serves=" + strings.Join(cls, "+"), "address=" + ep.GetAddress()}
	switch ep.GetNetwork() {
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_TCP:
		kv = append(kv, "network=tcp")
	case xtcp_config.ListenerNetwork_LISTENER_NETWORK_UNIX:
		kv = append(kv, "network=unix")
	}
	if m := ep.GetUnixSocketMode(); m != 0 {
		kv = append(kv, fmt.Sprintf("mode=%#o", m))
	}
	for _, a := range []struct {
		prefix, modeKey string
		auth            *xtcp_config.ListenerAuth
	}{{"", "auth", l.GetAuth()}, {"admin", "adminAuth", l.GetAdminAuth()}} {
		if a.auth == nil {
			continue
		}
		if m := a.auth.GetMode(); m != xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_UNSPECIFIED {
			kv = append(kv, a.modeKey+"="+listenerauth.ModeString(m))
		}
		if p := a.auth.GetRawTokenFile(); p != "" {
			kv = append(kv, fileKey(a.prefix, "tokenFile")+"="+p)
		}
		if p := a.auth.GetHmacSharedKeyFile(); p != "" {
			kv = append(kv, fileKey(a.prefix, "hmacKeyFile")+"="+p)
		}
	}
	return strings.Join(kv, ",")
}

// fileKey is "tokenFile" or, with prefix "admin", "adminTokenFile".
func fileKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + strings.ToUpper(key[:1]) + key[1:]
}

// ParseList parses the LISTENERS form: specs separated by ';'.
func ParseList(s string) ([]*xtcp_config.Listener, error) {
	var out []*xtcp_config.Listener
	for _, one := range strings.Split(s, ";") {
		if strings.TrimSpace(one) == "" {
			continue
		}
		l, err := ParseSpec(one)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}
