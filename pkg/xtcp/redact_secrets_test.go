package xtcp

import (
	"regexp"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// secretFieldName matches config field names that look like they hold a
// credential.
var secretFieldName = regexp.MustCompile(`(^|_)(key|token|secret|password|passwd|credential)s?($|_)`)

// notSecretFields are matches that are not credentials. Adding a name here is
// a reviewed decision; a new credential field must go in RedactSecrets.
var notSecretFields = map[protoreflect.FullName]bool{
	// Paths to credential files, not credentials: the config carries the
	// path and the listener reads the file when it starts.
	"xtcp_config.v1.ListenerAuth.raw_token_file":       true,
	"xtcp_config.v1.ListenerAuth.hmac_shared_key_file": true,
}

// fillSecretLike sets every string field of m (recursing into singular
// message fields, and into one appended element of each repeated message
// field) whose name matches secretFieldName, and returns their full names.
func fillSecretLike(m protoreflect.Message, depth int) []protoreflect.FullName {
	if depth > 8 {
		return nil
	}
	var names []protoreflect.FullName
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		switch {
		case fd.IsMap():
			continue
		case fd.IsList():
			if fd.Kind() == protoreflect.MessageKind {
				list := m.Mutable(fd).List()
				elem := list.NewElement()
				names = append(names, fillSecretLike(elem.Message(), depth+1)...)
				list.Append(elem)
			}
		case fd.Kind() == protoreflect.MessageKind:
			names = append(names, fillSecretLike(m.Mutable(fd).Message(), depth+1)...)
		case fd.Kind() == protoreflect.StringKind && secretFieldName.MatchString(string(fd.Name())):
			m.Set(fd, protoreflect.ValueOfString("secret-value"))
			names = append(names, fd.FullName())
		}
	}
	return names
}

// TestRedactSecretsCoversSecretLikeFields fails when a config field whose
// name looks like a credential is not blanked by RedactSecrets, which is what
// ConfigService.Get, the Set response and the soft-restart handoff all rely
// on. It also proves the walk finds the known credentials, so a regexp that
// silently matched nothing would fail too.
func TestRedactSecretsCoversSecretLikeFields(t *testing.T) {
	cfg := &xtcp_config.XtcpConfig{}
	names := fillSecretLike(cfg.ProtoReflect(), 0)

	known := map[protoreflect.FullName]bool{
		"xtcp_config.v1.XtcpConfig.s3_access_key":     false,
		"xtcp_config.v1.XtcpConfig.s3_secret_key":     false,
		"xtcp_config.v1.ListenerAuth.raw_token":       false,
		"xtcp_config.v1.ListenerAuth.hmac_shared_key": false,
	}
	for _, n := range names {
		if _, ok := known[n]; ok {
			known[n] = true
		}
	}
	for n, seen := range known {
		if !seen {
			t.Errorf("descriptor walk did not find known credential field %s", n)
		}
	}

	RedactSecrets(cfg)
	leaked := map[protoreflect.FullName]bool{}
	var check func(m protoreflect.Message, depth int)
	check = func(m protoreflect.Message, depth int) {
		if depth > 8 {
			return
		}
		m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			switch {
			case fd.IsMap():
			case fd.IsList():
				if fd.Kind() == protoreflect.MessageKind {
					for i := 0; i < v.List().Len(); i++ {
						check(v.List().Get(i).Message(), depth+1)
					}
				}
			case fd.Kind() == protoreflect.MessageKind:
				check(v.Message(), depth+1)
			case fd.Kind() == protoreflect.StringKind && v.String() == "secret-value" && !notSecretFields[fd.FullName()]:
				leaked[fd.FullName()] = true
			}
			return true
		})
	}
	check(cfg.ProtoReflect(), 0)
	for n := range leaked {
		t.Errorf("RedactSecrets does not blank %s: add it there (or, if it is not a credential, to notSecretFields)", n)
	}
}
