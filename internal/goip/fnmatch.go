package goip

import "fmt"

// fnmatch0 is glibc's fnmatch(pattern, s, 0), the call ifa_label_match_rta
// makes for `ip address show label PATTERN` (ip/ipaddress.c:1498-1510).
//
// It is not path.Match, which differs from glibc in exactly the places a
// label pattern reaches:
//
//   - With no FNM_PATHNAME, `*` and `?` match `/`. path.Match stops at it.
//   - An unterminated `[` is an ordinary character. path.Match reports
//     ErrBadPattern, and the label would then match nothing.
//   - A trailing backslash matches nothing. glibc's comment is "Trailing \
//     loses".
//
// The comparison is by byte: `ip` never calls setlocale, so glibc runs in the
// C locale, where every byte is one character.
//
// Bracket expressions take `!` or `^` to negate, a `]` first as a literal,
// ranges, and backslash escapes. The POSIX forms `[:class:]`, `[=c=]` and
// `[.c.]` are refused with ErrNotImplemented rather than matched as
// literals, which would answer wrongly. The refusal happens when the pattern
// is compiled, so it does not depend on what the pattern is matched against.
func fnmatch0(pattern, s string) (bool, error) {
	toks, err := fnmatchCompile(pattern)
	if err != nil {
		return false, err
	}
	return fnmatchToks(toks, s), nil
}

type fnmatchKind uint8

const (
	fnmLiteral fnmatchKind = iota
	fnmAny                 // ?
	fnmStar                // *
	fnmClass               // [...]
	fnmNever               // a trailing backslash
)

type fnmatchTok struct {
	kind   fnmatchKind
	lit    byte
	neg    bool
	ranges [][2]byte
}

func fnmatchCompile(p string) ([]fnmatchTok, error) {
	var toks []fnmatchTok
	for i := 0; i < len(p); i++ {
		switch c := p[i]; c {
		case '\\':
			if i+1 == len(p) {
				toks = append(toks, fnmatchTok{kind: fnmNever})
				continue
			}
			i++
			toks = append(toks, fnmatchTok{kind: fnmLiteral, lit: p[i]})
		case '*':
			if n := len(toks); n == 0 || toks[n-1].kind != fnmStar {
				toks = append(toks, fnmatchTok{kind: fnmStar})
			}
		case '?':
			toks = append(toks, fnmatchTok{kind: fnmAny})
		case '[':
			tok, next, ok, err := fnmatchBracket(p, i+1)
			if err != nil {
				return nil, err
			}
			if !ok {
				toks = append(toks, fnmatchTok{kind: fnmLiteral, lit: '['})
				continue
			}
			toks = append(toks, tok)
			i = next - 1
		default:
			toks = append(toks, fnmatchTok{kind: fnmLiteral, lit: c})
		}
	}
	return toks, nil
}

// fnmatchBracket parses the bracket expression starting after its `[`. It
// returns the index after the closing `]`, or ok false when there is none,
// in which case glibc reads the `[` as itself.
func fnmatchBracket(p string, j int) (fnmatchTok, int, bool, error) {
	tok := fnmatchTok{kind: fnmClass}
	if j < len(p) && (p[j] == '!' || p[j] == '^') {
		tok.neg = true
		j++
	}
	for first := true; j < len(p); first = false {
		c := p[j]
		if c == ']' && !first {
			return tok, j + 1, true, nil
		}
		if c == '[' && j+1 < len(p) && (p[j+1] == ':' || p[j+1] == '=' || p[j+1] == '.') {
			return fnmatchTok{}, 0, false, fmt.Errorf("label pattern %q: POSIX bracket class: %w", p, ErrNotImplemented)
		}
		lo, nj, ok := fnmatchBracketChar(p, j)
		if !ok {
			return fnmatchTok{}, 0, false, nil
		}
		j = nj
		hi := lo
		if j+1 < len(p) && p[j] == '-' && p[j+1] != ']' {
			hi, nj, ok = fnmatchBracketChar(p, j+1)
			if !ok {
				return fnmatchTok{}, 0, false, nil
			}
			j = nj
		}
		tok.ranges = append(tok.ranges, [2]byte{lo, hi})
	}
	return fnmatchTok{}, 0, false, nil
}

// fnmatchBracketChar reads one possibly escaped character of a bracket
// expression.
func fnmatchBracketChar(p string, j int) (byte, int, bool) {
	if p[j] == '\\' {
		j++
		if j >= len(p) {
			return 0, 0, false
		}
	}
	return p[j], j + 1, true
}

func (t *fnmatchTok) matchByte(c byte) bool {
	switch t.kind {
	case fnmLiteral:
		return c == t.lit
	case fnmAny:
		return true
	case fnmClass:
		in := false
		for _, r := range t.ranges {
			if r[0] <= c && c <= r[1] {
				in = true
				break
			}
		}
		return in != t.neg
	case fnmStar, fnmNever:
		// A star is consumed by fnmatchToks before it gets here, and a never
		// token matches nothing.
		return false
	}
	return false
}

// fnmatchToks matches with the usual single backtrack point: the most recent
// star, which is enough because a later star subsumes an earlier one.
func fnmatchToks(toks []fnmatchTok, s string) bool {
	ti, si := 0, 0
	starTi, starSi := -1, 0
	for si < len(s) {
		switch {
		case ti < len(toks) && toks[ti].kind == fnmStar:
			starTi, starSi = ti, si
			ti++
		case ti < len(toks) && toks[ti].matchByte(s[si]):
			ti++
			si++
		case starTi >= 0:
			starSi++
			si = starSi
			ti = starTi + 1
		default:
			return false
		}
	}
	for ti < len(toks) && toks[ti].kind == fnmStar {
		ti++
	}
	return ti == len(toks)
}
