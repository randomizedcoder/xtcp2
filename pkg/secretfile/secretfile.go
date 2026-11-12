// Package secretfile reads credentials from files (the Docker/Kubernetes
// `_FILE` convention), refusing files that would not keep them secret.
package secretfile

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrSecretFile marks a credential file that cannot be used: missing,
// unreadable, not a regular file, or accessible by group or others. Callers
// treat it as a configuration error and stop, rather than run with the
// credential empty.
var ErrSecretFile = errors.New("secret file")

// modeMask is the permission bits a secret file must not have: any access
// for group or others.
const modeMask = 0o077

// Read returns the trimmed contents of the credential file at path. label
// names where the path came from (an env var, a flag field) for the error.
// Symlinks are followed, as Kubernetes secret volumes need.
func Read(label, path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s=%q: %w", ErrSecretFile, label, path, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s=%q is not a regular file", ErrSecretFile, label, path)
	}
	if perm := fi.Mode().Perm(); perm&modeMask != 0 {
		return "", fmt.Errorf("%w: %s=%q has mode %04o; a secret file must not be accessible by group or "+
			"others (chmod 0400; Kubernetes secret defaultMode: 0400)", ErrSecretFile, label, path, perm)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: %s=%q: %w", ErrSecretFile, label, path, err)
	}
	return strings.TrimSpace(string(b)), nil
}
