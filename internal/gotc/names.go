package gotc

import (
	"errors"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// tc -nm: tc_util.c cls_names_init over lib/names.c's db_names_load. The
// database maps a class id to a name for print_tc_classid.

// namesDB is struct db_names: a hash whose chains put the newest entry
// first, so a later line for an id hides an earlier one, and a later file
// the earlier.
type namesDB struct{ byID map[uint32]string }

// lookup is id_to_name's hit: the name, or false.
func (db *namesDB) lookup(id uint32) (string, bool) {
	if db == nil {
		return "", false
	}
	n, ok := db.byID[id]
	return n, ok
}

// fgetsBufCst is read_id_name's buf: fgets keeps 511 bytes a call, so a
// longer line is read as several.
const fgetsBufCst = 512

// errNamesNoent is db_names_load's -ENOENT, which it returns for any
// fopen failure.
var errNamesNoent = errors.New("names: cannot open")

// errNamesCorrupt is its -1: a line read_id_name cannot parse.
var errNamesCorrupt = errors.New("names: corrupted")

// fgets splits content as successive fgets(buf, 512) calls return it.
func fgets(content string) []string {
	var out []string
	for content != "" {
		n := len(content)
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			n = i + 1
		}
		n = min(n, fgetsBufCst-1)
		out = append(out, content[:n])
		content = content[n:]
	}
	return out
}

// readIDName is one line of read_id_name: skip blanks and comments (done,
// false), then "%x:%x %s", "0x%x %s" or "%d %s" (the " #" forms match
// the same lines). A line none of them reads is the corruption, err.
func readIDName(line string) (id int32, name string, ok bool, err error) {
	line = cstr(line)
	p := strings.TrimLeft(line, " \t")
	if p == "" || p[0] == '#' || p[0] == '\n' {
		return 0, "", false, nil
	}
	if maj, rest, ok1 := scanHex(p, 0); ok1 {
		if rest, ok2 := strings.CutPrefix(rest, ":"); ok2 {
			if minor, rest, ok3 := scanHex(rest, 0); ok3 {
				if n, _, ok4 := scanStr(rest); ok4 {
					// (maj << 16) | min in int: gcc's wrap.
					return cint.Int(maj<<16 | minor), n, true, nil
				}
			}
		}
	}
	if rest, ok1 := strings.CutPrefix(p, "0x"); ok1 {
		if v, rest, ok2 := scanHex(rest, 0); ok2 {
			if n, _, ok3 := scanStr(rest); ok3 {
				return cint.Int(v), n, true, nil
			}
		}
	}
	if v, rest, ok1 := scanDec(p); ok1 {
		if n, _, ok2 := scanStr(rest); ok2 {
			return v, n, true, nil
		}
	}
	return 0, p, false, errNamesCorrupt
}

// cstr is s as the C string functions see it: up to its first NUL.
func cstr(s string) string {
	s, _, _ = strings.Cut(s, "\x00")
	return s
}

// loadNames is db_names_load: each line's entry joins db, a negative id
// skipped, until a corrupt line, whose report ends the load with the
// entries before it kept.
func (r *runner) loadNames(db *namesDB, path string) error {
	content, err := r.sys.readFile(path)
	if err != nil {
		return errNamesNoent
	}
	for _, line := range fgets(content) {
		id, name, ok, err := readIDName(line)
		if err != nil {
			r.errf("Database %s is corrupted at %s\n", path, name)
			return err
		}
		if !ok || id < 0 {
			continue
		}
		db.byID[cint.Bits(id)] = name
	}
	return nil
}

// namesInit is cls_names_init(conf_file): the -cf file (not found is
// "Can't open" and -1), then CONF_ETC_DIR's tc_cls, else CONF_USR_DIR's.
// When that last load fails the whole database is dropped, the -cf file's
// names with it (Q33), and -nm prints numbers; the result is still 0.
func (r *runner) namesInit() error {
	db := &namesDB{byID: map[uint32]string{}}
	if r.confFile != nil {
		if err := r.loadNames(db, *r.confFile); errors.Is(err, errNamesNoent) {
			r.errf("Can't open class names file: %s\n", *r.confFile)
			return retCode(-1)
		}
	}
	err := r.loadNames(db, confEtcDirCst+"/"+clsNamesDBName)
	if errors.Is(err, errNamesNoent) {
		err = r.loadNames(db, r.sys.usrDir+"/"+clsNamesDBName)
	}
	if err != nil {
		r.names = nil
		return nil
	}
	r.names = db
	return nil
}
