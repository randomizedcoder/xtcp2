package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// The soft restart's handoff to the image it execs into.
//
//   - The config, its secrets blanked, goes in reconfigureEnvKey. The
//     environment stays the readable record of what the daemon runs, so
//     /proc/<pid>/environ shows the config but not the secrets, which the
//     operator may have kept out of it with the *_FILE variables.
//   - The secrets go in a sealed memfd, inherited across the exec, whose
//     descriptor number is in reconfigureMemfdFDKey. It is anonymous
//     memory: nothing on disk, and nothing in the environment but a number.
//   - argv is rebuilt from the flags the command line set, not copied from
//     os.Args, so it carries only flags this binary defines.

const (
	// reconfigureMemfdFDKey names the descriptor that carries the
	// config's secrets across the exec.
	reconfigureMemfdFDKey = "XTCP_CONFIG_SECRETS_FD"
	// reconfigureMemfdName is the memfd's name, as /proc/<pid>/fd shows it.
	reconfigureMemfdName = "xtcp2-reconfigure-secrets"
	// reconfigureSecretsMaxBytes bounds the read: six strings, the listener
	// ones validated at 4 KiB each.
	reconfigureSecretsMaxBytes = 1 << 20
	// reconfigureSecretSeals are the seals the memfd carries: once written,
	// its contents can't change and no seal can be removed.
	reconfigureSecretSeals = unix.F_SEAL_SEAL | unix.F_SEAL_SHRINK | unix.F_SEAL_GROW | unix.F_SEAL_WRITE
)

var errReconfigureSecrets = errors.New("reconfigure secrets")

// splitSecrets returns cfg with its secrets blanked by ConfigService's
// xtcp.RedactSecrets, and a config holding only them. mergeSecrets is its
// inverse. TestSplitSecretsMatchesRedactSecrets fails if the fields carried
// here and the fields RedactSecrets blanks drift apart.
func splitSecrets(cfg *xtcp_config.XtcpConfig) (redacted, secrets *xtcp_config.XtcpConfig) {
	redacted, ok := proto.Clone(cfg).(*xtcp_config.XtcpConfig)
	if !ok {
		redacted = &xtcp_config.XtcpConfig{}
	}
	xtcp.RedactSecrets(redacted)
	secrets = &xtcp_config.XtcpConfig{
		S3SecretKey:       cfg.GetS3SecretKey(),
		S3AccessKey:       cfg.GetS3AccessKey(),
		ListenerAuth:      authSecrets(cfg.GetListenerAuth()),
		AdminListenerAuth: authSecrets(cfg.GetAdminListenerAuth()),
	}
	return redacted, secrets
}

// authSecrets is a listener auth block's secrets alone; nil when it has none.
func authSecrets(a *xtcp_config.ListenerAuth) *xtcp_config.ListenerAuth {
	if a.GetRawToken() == "" && a.GetHmacSharedKey() == "" {
		return nil
	}
	return &xtcp_config.ListenerAuth{RawToken: a.GetRawToken(), HmacSharedKey: a.GetHmacSharedKey()}
}

// mergeSecrets puts splitSecrets' secrets back into its redacted config.
func mergeSecrets(cfg, secrets *xtcp_config.XtcpConfig) {
	cfg.S3SecretKey = secrets.GetS3SecretKey()
	cfg.S3AccessKey = secrets.GetS3AccessKey()
	cfg.ListenerAuth = mergeAuthSecrets(cfg.ListenerAuth, secrets.GetListenerAuth())
	cfg.AdminListenerAuth = mergeAuthSecrets(cfg.AdminListenerAuth, secrets.GetAdminListenerAuth())
}

func mergeAuthSecrets(a, s *xtcp_config.ListenerAuth) *xtcp_config.ListenerAuth {
	if s == nil {
		return a
	}
	if a == nil {
		a = &xtcp_config.ListenerAuth{}
	}
	a.RawToken, a.HmacSharedKey = s.GetRawToken(), s.GetHmacSharedKey()
	return a
}

// secretsMemfd writes b to a sealed memfd the exec'd image inherits: it is
// made without MFD_CLOEXEC, and syscall.Exec closes only close-on-exec
// descriptors. b past reconfigureSecretsMaxBytes is refused here, since
// the exec'd image would refuse it after this one is gone.
func secretsMemfd(b []byte) (f *os.File, fd int, err error) {
	if len(b) > reconfigureSecretsMaxBytes {
		return nil, -1, fmt.Errorf("%w: %d bytes, past %d", errReconfigureSecrets, len(b), reconfigureSecretsMaxBytes)
	}
	fd, err = unix.MemfdCreate(reconfigureMemfdName, unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, -1, fmt.Errorf("%w: memfd_create: %w", errReconfigureSecrets, err)
	}
	f = os.NewFile(uintptr(fd), reconfigureMemfdName)
	if _, err = f.Write(b); err == nil {
		_, err = unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, reconfigureSecretSeals)
	}
	if err != nil {
		return nil, -1, errors.Join(fmt.Errorf("%w: %w", errReconfigureSecrets, err), f.Close())
	}
	return f, fd, nil
}

// loadReconfigureSecrets reads the secrets a soft restart carried in a
// memfd into cfg, and closes it. It does nothing when the restart carried
// none. The descriptor must be a memfd with every seal secretsMemfd adds,
// so an inherited stdin or a stray file is refused rather than read; a
// memfd that is refused is closed too.
func loadReconfigureSecrets(cfg *xtcp_config.XtcpConfig) (err error) {
	v, present := os.LookupEnv(reconfigureMemfdFDKey)
	if !present || v == "" {
		return nil
	}
	fd, err := strconv.ParseUint(v, 10, 31)
	if err != nil || fd < 3 {
		return fmt.Errorf("%w: %s=%q is not a descriptor", errReconfigureSecrets, reconfigureMemfdFDKey, v)
	}
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil {
		return fmt.Errorf("%w: descriptor %d is not a memfd: %w", errReconfigureSecrets, fd, err)
	}
	// A memfd named by the key is the carry's, so it is closed from here
	// on, read or refused.
	f := os.NewFile(uintptr(fd), reconfigureMemfdName)
	defer func() {
		err = errors.Join(err, f.Close())
	}()
	if seals&reconfigureSecretSeals != reconfigureSecretSeals {
		return fmt.Errorf("%w: descriptor %d is not sealed (seals %#x)", errReconfigureSecrets, fd, seals)
	}
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%w: %w", errReconfigureSecrets, err)
	}
	if st.Size() > reconfigureSecretsMaxBytes {
		return fmt.Errorf("%w: %d bytes, past %d", errReconfigureSecrets, st.Size(), reconfigureSecretsMaxBytes)
	}
	b, err := io.ReadAll(io.NewSectionReader(f, 0, st.Size()))
	if err != nil {
		return fmt.Errorf("%w: %w", errReconfigureSecrets, err)
	}
	secrets := &xtcp_config.XtcpConfig{}
	if err := protojson.Unmarshal(b, secrets); err != nil {
		return fmt.Errorf("%w: %w", errReconfigureSecrets, err)
	}
	mergeSecrets(cfg, secrets)
	return os.Unsetenv(reconfigureMemfdFDKey)
}

// reexecArgv is the soft restart's argv: exe, then each flag the command
// line set, as -name=value, in flag order. Every flag is a standard
// flag.Value, whose String parses back to the same value.
func reexecArgv(exe string, fs *flag.FlagSet) []string {
	argv := []string{exe}
	fs.Visit(func(f *flag.Flag) {
		argv = append(argv, "-"+f.Name+"="+f.Value.String())
	})
	return argv
}
