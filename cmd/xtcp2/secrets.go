package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/secretfile"
)

// flavorProduction is "true" in the production (slim) flavors, set at link
// time by the Nix flavor matrix: -X main.flavorProduction=true. It is a
// build-time property, not a flag, so a deployment cannot switch it off.
var flavorProduction string

// productionBuild reports whether this binary is a production flavor.
func productionBuild() bool { return flavorProduction == "true" }

// errUsage marks configuration the operator must change; exit 64.
var errUsage = errors.New("usage")

// secretFlag pairs a flag that carries a credential with the _FILE env var
// that should carry it instead.
type secretFlag struct {
	flag    string
	fileEnv string
}

// secretFlags is every flag whose value is a credential. A value given this
// way is visible in ps, /proc/<pid>/cmdline and docker inspect.
var secretFlags = []secretFlag{
	{"s3AccessKey", "S3_ACCESS_KEY_FILE"},
	{"s3SecretKey", "S3_SECRET_KEY_FILE"},
	{"listenerRawToken", "LISTENER_RAW_TOKEN_FILE"},
	{"listenerHMACSharedKey", "LISTENER_HMAC_SHARED_KEY_FILE"},
	{"listenerAdminRawToken", "LISTENER_ADMIN_RAW_TOKEN_FILE"},
	{"listenerAdminHMACSharedKey", "LISTENER_ADMIN_HMAC_SHARED_KEY_FILE"},
}

// checkSecretFlags looks at the flags actually given on the command line,
// whatever the environment also says. A production build refuses a non-empty
// credential flag; any other build warns once per flag.
func checkSecretFlags(fs *flag.FlagSet, production bool) error {
	byName := make(map[string]secretFlag, len(secretFlags))
	for _, s := range secretFlags {
		byName[s.flag] = s
	}
	var given []secretFlag
	fs.Visit(func(f *flag.Flag) {
		if s, ok := byName[f.Name]; ok && f.Value.String() != "" {
			given = append(given, s)
		}
	})
	for _, s := range given {
		if production {
			return fmt.Errorf("%w: -%s puts a secret on the command line, where ps, /proc/<pid>/cmdline and "+
				"docker inspect show it, and this production build refuses it. Put the value in a file "+
				"readable only by xtcp2 and set %s to its path", errUsage, s.flag, s.fileEnv)
		}
		log.Printf("WARNING: -%s puts a secret on the command line (visible in ps, /proc/<pid>/cmdline, "+
			"docker inspect); set %s instead. Production builds refuse this.", s.flag, s.fileEnv)
	}
	return nil
}

// readSecretFile reads a credential from the file a _FILE env var names; see
// secretfile.Read. A file it cannot safely use is a usage error.
func readSecretFile(env, path string) (string, error) {
	v, err := secretfile.Read(env, path)
	if err != nil {
		return "", fmt.Errorf("%w: %w", errUsage, err)
	}
	return v, nil
}

// secretFileTarget is one _FILE env var and where its value goes.
type secretFileTarget struct {
	env string
	set func(*xtcp_config.XtcpConfig, string)
}

func listenerAuth(c *xtcp_config.XtcpConfig) *xtcp_config.ListenerAuth {
	if c.ListenerAuth == nil {
		c.ListenerAuth = &xtcp_config.ListenerAuth{}
	}
	return c.ListenerAuth
}

func adminListenerAuth(c *xtcp_config.XtcpConfig) *xtcp_config.ListenerAuth {
	if c.AdminListenerAuth == nil {
		c.AdminListenerAuth = &xtcp_config.ListenerAuth{}
	}
	return c.AdminListenerAuth
}

var listenerSecretFiles = []secretFileTarget{
	{"LISTENER_RAW_TOKEN_FILE", func(c *xtcp_config.XtcpConfig, v string) { listenerAuth(c).RawToken = v }},
	{"LISTENER_HMAC_SHARED_KEY_FILE", func(c *xtcp_config.XtcpConfig, v string) { listenerAuth(c).HmacSharedKey = v }},
	{"LISTENER_ADMIN_RAW_TOKEN_FILE", func(c *xtcp_config.XtcpConfig, v string) { adminListenerAuth(c).RawToken = v }},
	{"LISTENER_ADMIN_HMAC_SHARED_KEY_FILE", func(c *xtcp_config.XtcpConfig, v string) { adminListenerAuth(c).HmacSharedKey = v }},
}

var s3SecretFiles = []secretFileTarget{
	{"S3_ACCESS_KEY_FILE", func(c *xtcp_config.XtcpConfig, v string) { c.S3AccessKey = v }},
	{"S3_SECRET_KEY_FILE", func(c *xtcp_config.XtcpConfig, v string) { c.S3SecretKey = v }},
}

// applySecretFileTargets reads each set _FILE env var into c. Applied after
// the inline env overrides, so a file wins over the inline variable.
func applySecretFileTargets(c *xtcp_config.XtcpConfig, targets []secretFileTarget) error {
	for _, t := range targets {
		path, ok := os.LookupEnv(t.env)
		if !ok || path == "" {
			continue
		}
		v, err := readSecretFile(t.env, path)
		if err != nil {
			return err
		}
		t.set(c, v)
	}
	return nil
}

// applyListenerSecretFiles reads the listener credential _FILE variables.
func applyListenerSecretFiles(c *xtcp_config.XtcpConfig) error {
	return applySecretFileTargets(c, listenerSecretFiles)
}

// applySecretFiles reads every credential _FILE variable into c.
func applySecretFiles(c *xtcp_config.XtcpConfig) error {
	if err := applySecretFileTargets(c, listenerSecretFiles); err != nil {
		return err
	}
	return applySecretFileTargets(c, s3SecretFiles)
}
