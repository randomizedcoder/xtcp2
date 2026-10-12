package gordma

import (
	"strconv"
	"strings"
)

// filterDef is struct filters: a key a resource accepts, whether its value
// is numeric, and whether a plain number makes the request a doit.
type filterDef struct {
	name   string
	number bool
	doit   bool
}

// filterEntry is struct filter_entry: a key as the user typed it, which
// may be a prefix of the filter's name, and the lowered value.
type filterEntry struct {
	key, value string
	doit       bool
}

// filterNumberChars is add_filter's cset.
const filterNumberChars = "1234567890,-"

// buildFilter is rd_build_filter: the arguments as key/value pairs, each
// admitted by addFilter. With no valid filters, the arguments are left
// alone.
func (r *rd) buildFilter(valid []filterDef) error {
	if valid == nil || len(r.args) == 0 {
		return nil
	}
	if len(r.args) == 1 {
		r.errf("No filter data was supplied to filter option %s\n", r.args[0])
		return errFail
	}
	if len(r.args)%2 != 0 {
		r.errf("There is filter option without data\n")
		return errFail
	}
	for i := 0; i < len(r.args); i += 2 {
		if err := r.addFilter(r.args[i], r.args[i+1], valid); err != nil {
			return err
		}
	}
	return nil
}

// addFilter is add_filter.
func (r *rd) addFilter(key, value string, valid []filterDef) error {
	var def *filterDef
	for i := range valid {
		if strcmpx(key, valid[i].name) {
			def = &valid[i]
			break
		}
	}
	if def == nil {
		r.errf("Unsupported filter option: %s\n", key)
		return errFail
	}
	if def.number && !onlyChars(value, filterNumberChars) {
		r.errf("%s filter accepts \"%s\" characters only\n", key, filterNumberChars)
		return errFail
	}
	r.filters = append(r.filters, filterEntry{key: key, value: strings.ToLower(value), doit: def.doit && strtolWhole(value)})
	return nil
}

func onlyChars(s, set string) bool {
	for i := range len(s) {
		if strings.IndexByte(set, s[i]) < 0 {
			return false
		}
	}
	return true
}

// strtolWhole reports what add_filter's strtol check does: that s is
// entirely a base-10 long with no ERANGE. The empty string passes, as
// strtol's end pointer is then s itself, at its NUL.
func strtolWhole(s string) bool {
	if s == "" {
		return true
	}
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	_, err := strconv.ParseInt(t, 10, 64)
	return err == nil
}

// doitIndex is rd_doit_index: the first doit filter's value, as atoi.
func (r *rd) doitIndex() (uint32, bool) {
	for _, f := range r.filters {
		if f.doit {
			return cAtoiSigned(f.value), true
		}
	}
	return 0, false
}

// cAtoiSigned is atoi with an optional sign, as uint32.
func cAtoiSigned(s string) uint32 {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if s != "" && (s[0] == '-' || s[0] == '+') {
		neg = s[0] == '-'
		s = s[1:]
	}
	v := cAtoi(s)
	if neg {
		return -v
	}
	return v
}

// keyExists is rd_check_is_key_exist.
func (r *rd) keyExists(key string) bool {
	for _, f := range r.filters {
		if strcmpx(f.key, key) {
			return true
		}
	}
	return false
}

// isFiltered is rd_is_filtered_attr: an absent attribute is filtered out
// when the user filters on it; a present one when its value is not in the
// filter's list of numbers and ranges.
func (r *rd) isFiltered(key string, v uint32, present bool) bool {
	if !present {
		return r.keyExists(key)
	}
	for _, f := range r.filters {
		if strcmpx(f.key, key) {
			return !numberMatches(f.value, v)
		}
	}
	return false
}

// numberMatches is rd_check_is_filtered's walk: "a", "a,b" and "a-b",
// and their mixtures. The range test itself is exclusive, but each end is
// also a number of the list, so "a-b" matches a through b.
func numberMatches(s string, v uint32) bool {
	var left, cur uint32
	rangeCheck := false
	for i := 0; i < len(s); {
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			cur = cAtoi(s[i:j])
			if cur == v || (rangeCheck && left < v && v < cur) {
				return true
			}
			rangeCheck = false
			i = j
			continue
		}
		if s[i] == '-' {
			left = cur
			rangeCheck = true
		}
		i++
	}
	return false
}

// isStringFiltered is rd_is_string_filtered_attr, which checks the value
// whether or not the attribute was there: v is then the caller's default.
func (r *rd) isStringFiltered(key, v string) bool {
	for _, f := range r.filters {
		if !strcmpx(f.key, key) {
			continue
		}
		for _, tok := range strings.Split(f.value, ",") {
			if tok != "" && len(tok) == len(v) && strings.EqualFold(tok, v) {
				return false
			}
		}
		return true
	}
	return false
}

// setArgToDevname is rd_set_arg_to_devname: the arguments from the one
// after "dev" or "link", or none.
func (r *rd) setArgToDevname() error {
	for len(r.args) > 0 {
		a := r.args[0]
		r.args = r.args[1:]
		if strcmpx(a, "dev") || strcmpx(a, "link") {
			if len(r.args) == 0 {
				r.errf("No device name was supplied\n")
				return errFail
			}
			return nil
		}
	}
	return nil
}
