package main

import (
	"fmt"
	"io/fs"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// kselftest mines the ip commands of the kernel's fib selftests
// (tools/testing/selftests/net/fib_tests.sh, fib_nexthops.sh and
// fib_rule_tests.sh) into rows. It reads the scripts statically, without
// running a shell:
//
//   - each test function, after the functions the script's main loop runs
//     before it, is one group; calls to the script's own functions are
//     inlined with their arguments, and their locals are scoped;
//   - a `for` loop over a literal list, or over `$(seq A B)`, runs its body
//     once per word; one over a computed list is skipped;
//   - lines under if, case, while or until are skipped, since which branch
//     runs is not known statically;
//   - `ip -netns <the test's namespace>` loses the option, the row running
//     in its own namespace; a command in another namespace is skipped, and
//     a device the selftest moves to another namespace stays here,
//     unaddressed;
//   - `log_test $? N "msg"` right after a command says the command's rc
//     is N, and N's sign gives the row its positive: or negative: class;
//   - a command needing anything else (command substitution, a pipe, an
//     unresolved variable) is skipped, with the reason.
//
// Each route, rule or nexthop command becomes a row whose pre steps are
// every ip command of its group before it. Every skipped line is written
// to the skipped file, so nothing is dropped silently.

// ksFile is one selftest script and how its main loop runs its tests.
type ksFile struct {
	Name       string   // under tools/testing/selftests/net
	Space      string   // the rows' id prefix
	OwnNS      []string // the variables naming the namespace the tests run in
	Prelude    []string // the functions the main loop runs before each test
	TestsVar   string   // the top-level variable listing the tests
	CaseMapped bool     // the main loop's case maps each listed name to a function
}

var ksFiles = []ksFile{
	{Name: "fib_tests.sh", Space: "fibt", OwnNS: []string{"ns1"}, TestsVar: "TESTS", CaseMapped: true},
	{Name: "fib_nexthops.sh", Space: "fibnh", OwnNS: []string{"me"}, Prelude: []string{"setup"}, TestsVar: "ALL_TESTS"},
	{Name: "fib_rule_tests.sh", Space: "fibrule", OwnNS: []string{"testns"}, Prelude: []string{"setup"},
		TestsVar: "TESTS", CaseMapped: true},
}

// ksRowObjects are the ip objects whose commands become rows; every other
// ip command is only ever a pre step.
var ksRowObjects = []string{"route", "rule", "nexthop"}

// ksNsSetup is what lib.sh's setup_ns does to every namespace beyond the
// loopback the workbench's ns-add already brings up.
const ksNsSetup = "sysctl -wq net.ipv4.conf.all.rp_filter=0 && sysctl -wq net.ipv4.conf.default.rp_filter=0"

// ksTolerated are the exit statuses a pre step the selftest does not
// require to succeed may end with: iproute2's 1, 2 and 255.
var ksTolerated = []int{1, 2, 255}

// ksModules maps a link kind a pre step creates to the module it needs.
var ksModules = map[string]string{
	"dummy": "dummy", "veth": "veth", "vrf": "vrf", "bridge": "bridge",
	"macvlan": "macvlan", "ipvlan": "ipvlan", "vxlan": "vxlan",
}

// ksNewer are keywords the selftests use that the workbench's iproute2
// (7.2.0) predates, with the commit that added each. ip refuses them before
// sending, so a row using one states no rc and a pre step using one may
// fail.
var ksNewer = map[string]string{
	"dst_port": "iproute2 ad2b2d95 (ip nexthop: support fdb destination port), after 7.2.0",
}

// ksNewerIn is the ksNewer entry a command uses, or "".
func ksNewerIn(cmd string) string {
	for _, word := range strings.Fields(cmd) {
		if c, ok := ksNewer[word]; ok {
			return word + " needs " + c
		}
	}
	return ""
}

// ksMaxDepth bounds inlining, so a recursive helper cannot loop.
const ksMaxDepth = 6

// shLine is one logical line of a script: continuations joined, comments
// dropped. Col0 says it began in the first column, which is how the
// scripts close their functions.
type shLine struct {
	No   int
	Text string
	Col0 bool
}

// shScan reports whether s leaves a quote open, and where an unquoted
// comment starts (-1 if none).
func shScan(s string, inDouble, inSingle bool) (dbl, sgl bool, comment int) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '\\':
			i++
		case c == '"':
			inDouble = !inDouble
		case inDouble:
		case c == '\'':
			inSingle = true
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return inDouble, inSingle, i
		}
	}
	return inDouble, inSingle, -1
}

// shLines splits a script into logical lines. A line ending in a backslash,
// or inside an open quote, continues on the next, joined by a space.
func shLines(src string) []shLine {
	var out []shLine
	var cur shLine
	var buf strings.Builder
	open, dbl, sgl := false, false, false
	for i, raw := range strings.Split(src, "\n") {
		if !open {
			cur = shLine{No: i + 1, Col0: raw != "" && raw[0] != ' ' && raw[0] != '\t'}
			buf.Reset()
		}
		var comment int
		wasDbl, wasSgl := dbl, sgl
		dbl, sgl, comment = shScan(raw, dbl, sgl)
		if comment >= 0 && !wasDbl && !wasSgl {
			raw = raw[:comment]
		}
		text := strings.TrimSpace(raw)
		if open {
			buf.WriteByte(' ')
		}
		cont := strings.HasSuffix(text, `\`) && !strings.HasSuffix(text, `\\`)
		buf.WriteString(strings.TrimSuffix(text, `\`))
		open = cont || dbl || sgl
		if open {
			continue
		}
		cur.Text = strings.Join(strings.Fields(buf.String()), " ")
		if cur.Text != "" {
			out = append(out, cur)
		}
	}
	return out
}

var shFuncRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\(\)\s*(\{)?$`)

// shFuncs splits logical lines into function bodies and the top level.
func shFuncs(lines []shLine) (funcs map[string][]shLine, top []shLine) {
	funcs = map[string][]shLine{}
	for i := 0; i < len(lines); i++ {
		m := shFuncRE.FindStringSubmatch(lines[i].Text)
		if m == nil || !lines[i].Col0 {
			top = append(top, lines[i])
			continue
		}
		if m[2] == "" && i+1 < len(lines) && lines[i+1].Text == "{" {
			i++
		}
		var body []shLine
		for i++; i < len(lines) && (lines[i].Text != "}" || !lines[i].Col0); i++ {
			body = append(body, lines[i])
		}
		funcs[m[1]] = body
	}
	return funcs, top
}

// shWords splits a command into words the way the shell would after
// expansion: quotes group and are removed, a backslash escapes. ok is false
// if a quote is left open.
func shWords(s string) (words []string, ok bool) {
	var w strings.Builder
	in, quote := false, byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == '\'':
			w.WriteByte(c)
		case c == '\\' && i+1 < len(s):
			i++
			w.WriteByte(s[i])
			in = true
		case quote == 0 && (c == '"' || c == '\''):
			quote, in = c, true
		case quote == 0 && (c == ' ' || c == '\t'):
			if in {
				words = append(words, w.String())
				w.Reset()
				in = false
			}
		default:
			w.WriteByte(c)
			in = true
		}
	}
	if in {
		words = append(words, w.String())
	}
	return words, quote == 0
}

var shVarRE = regexp.MustCompile(`\$(\{(?:[A-Za-z_][A-Za-z0-9_]*|[0-9])\}|[A-Za-z_][A-Za-z0-9_]*|[0-9])`)

// shExpand replaces each variable vars knows; the rest are left as written.
func shExpand(s string, vars map[string]string) string {
	return shVarRE.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.Trim(m[1:], "{}")
		if v, ok := vars[name]; ok {
			return v
		}
		return m
	})
}

// ksStep is one ip command of a group, or a sysctl it runs in the test's
// namespace (PreOnly).
type ksStep struct {
	Line    int
	After   int // a skipped write before it, whose state it may need
	Func    string
	Cmd     string
	Strict  bool // the selftest requires it to succeed
	PreOnly bool
	RC      *int
	Msg     string
	Note    string
}

// ksSkip is a line with an ip command that has no step, and why.
type ksSkip struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Func   string `json:"func"`
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// ksWalker walks one group's functions, gathering its steps.
type ksWalker struct {
	file   *ksFile
	funcs  map[string][]shLine
	vars   map[string]string
	locals []map[string]*string // per inlined call: the outer values of its locals
	steps  []ksStep
	skips  []ksSkip
	cond   int  // depth of if, case, while and until around the line
	setE   bool // under set -e, where a failing command ends the test
	last   int  // the step the previous line made, or -1
	// moved maps a device the selftest moves to another namespace, and the
	// miner keeps here, to that namespace.
	moved map[string]string
	// lost is the line of the first write in the own namespace the miner
	// skipped, or 0. The rc a selftest states for a later step assumes it ran.
	lost int
}

func (w *ksWalker) skip(fn string, l shLine, reason string) {
	w.skips = append(w.skips, ksSkip{File: w.file.Name, Line: l.No, Func: fn, Text: l.Text, Reason: reason})
}

// mentionsIP says whether a line would run ip, or a function that might.
func (w *ksWalker) mentionsIP(text string) bool {
	if strings.Contains(text, "$IP") || strings.Contains(text, "${IP}") || strings.HasPrefix(text, "ip ") ||
		strings.Contains(text, "run_cmd") {
		return true
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return false
	}
	if strings.HasPrefix(words[0], "$") && strings.HasPrefix(shExpand(words[0], w.vars), "ip ") {
		return true // $NS_EXEC, which names ip netns exec
	}
	_, ok := w.funcs[words[0]]
	return ok
}

// walk runs a function body; it reports whether a return or exit ended it.
func (w *ksWalker) walk(fn string, body []shLine) bool {
	for i := 0; i < len(body); i++ {
		l := body[i]
		if strings.HasPrefix(l.Text, "for ") {
			i = w.forLoop(fn, body, i)
			continue
		}
		if strings.HasPrefix(l.Text, "if ") && !strings.HasSuffix(l.Text, "fi") && w.cond == 0 {
			var stop bool
			if i, stop = w.ifBlock(fn, body, i); stop {
				return true
			}
			continue
		}
		if w.condition(l.Text) {
			if w.mentionsIP(l.Text) {
				w.skip(fn, l, "a condition's test")
			}
			continue
		}
		if w.cond > 0 {
			if w.mentionsIP(l.Text) {
				w.skip(fn, l, "under a condition")
			}
			continue
		}
		if w.line(fn, l) {
			return true
		}
	}
	return false
}

// ifBlock walks an if's then branch, the path a selftest takes on a kernel
// and iproute2 that have what it probes for, unless the branch returns or
// exits, which makes it a failure check. elif and else branches are
// skipped. It returns the index of the fi, and whether a return ended the
// walk.
func (w *ksWalker) ifBlock(fn string, body []shLine, i int) (int, bool) {
	end, depth, split := i+1, 1, -1
	for ; end < len(body); end++ {
		t := body[end].Text
		switch first := firstWord(t); {
		case first == "if" && !strings.HasSuffix(t, "fi"):
			depth++
		case first == "fi" || first == "fi;":
			depth--
		case depth == 1 && split < 0 && (first == "else" || first == "elif"):
			split = end
		}
		if depth == 0 {
			break
		}
	}
	if w.mentionsIP(body[i].Text) {
		w.skip(fn, body[i], "a condition's test")
	}
	stop := min(end, len(body))
	thenEnd := stop
	if split >= 0 {
		thenEnd = split
	}
	start := min(i+1, thenEnd)
	if start < thenEnd && body[start].Text == "then" {
		start++
	}
	branch := body[start:thenEnd]
	failure := slices.ContainsFunc(branch, func(l shLine) bool {
		f := firstWord(l.Text)
		return f == "return" || f == "exit"
	})
	ended := false
	switch {
	case failure:
		w.skipIPLines(fn, branch, "under a failure check")
	case w.testsRC(body[i].Text):
		w.skipIPLines(fn, branch, "under a test of an earlier rc")
	default:
		ended = w.walk(fn, branch)
	}
	w.skipIPLines(fn, body[thenEnd:stop], "an else branch")
	return end, ended
}

// testsRC says whether an if's condition reads $?, or a variable a line
// set from it, so that which branch runs depends on how a command ended.
func (w *ksWalker) testsRC(cond string) bool {
	if strings.Contains(cond, "$?") {
		return true
	}
	for _, m := range shVarRE.FindAllString(cond, -1) {
		if w.vars[strings.Trim(m[1:], "{}")] == "$?" {
			return true
		}
	}
	return false
}

// skipIPLines records every line of lines that would run ip as skipped.
func (w *ksWalker) skipIPLines(fn string, lines []shLine, reason string) {
	for _, l := range lines {
		if w.mentionsIP(l.Text) {
			w.skip(fn, l, reason)
		}
	}
}

func firstWord(text string) string {
	first, _, _ := strings.Cut(text, " ")
	return first
}

// condition tracks if, case, while and until, and reports whether l is
// one of their keyword lines.
func (w *ksWalker) condition(text string) bool {
	first, _, _ := strings.Cut(text, " ")
	switch first {
	case "if":
		if !strings.HasSuffix(text, "fi") {
			w.cond++
		}
		return true
	case "case", "while", "until":
		if !strings.HasSuffix(text, "esac") && !strings.HasSuffix(text, "done") {
			w.cond++
		}
		return true
	case "fi", "esac", "done", "fi;", "done;":
		w.cond--
		return true
	case "then", "else", "elif", "do", ";;":
		return true
	}
	return strings.HasPrefix(text, "[ ") || strings.HasPrefix(text, "[[ ") || strings.HasPrefix(text, "test ")
}

// line runs one unconditional line; it reports a return or exit.
func (w *ksWalker) line(fn string, l shLine) bool {
	last := w.last
	w.last = -1
	words := strings.Fields(l.Text)
	switch first := words[0]; {
	case first == "return" || first == "exit":
		return true
	case first == "set":
		if len(words) > 1 && (words[1] == "-e" || words[1] == "+e") {
			w.setE = words[1] == "-e"
		}
	case first == "log_test":
		w.logTest(last, l.Text)
	case first == "run_cmd":
		w.runCmd(fn, l)
	case w.assign(l.Text):
		if strings.HasSuffix(l.Text, "=$?") {
			w.last = last
		}
	case first == "local" || first == "declare":
		for _, name := range words[1:] {
			w.local(name)
			delete(w.vars, name)
		}
	default:
		if _, ok := w.funcs[first]; ok {
			w.inline(fn, l, words)
			return false
		}
		if w.mentionsIP(l.Text) {
			w.ipLine(fn, l, l.Text, true)
		}
	}
	return false
}

var shAssignRE = regexp.MustCompile(`^(?:(local|declare|export|readonly)\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// assign handles `[local] name=value`, reporting whether l was one. A value
// that can't be known statically unsets the name.
func (w *ksWalker) assign(text string) bool {
	m := shAssignRE.FindStringSubmatch(text)
	if m == nil {
		return false
	}
	if m[1] == "local" || m[1] == "declare" {
		w.local(m[2])
	}
	v := strings.ReplaceAll(m[3], "$(which ip)", "ip")
	if v == "$?" {
		w.vars[m[2]] = v
		return true
	}
	if strings.Contains(v, "$(") || strings.Contains(v, "`") || strings.ContainsAny(v, "|;&") {
		delete(w.vars, m[2])
		return true
	}
	words, ok := shWords(shExpand(v, w.vars))
	if !ok {
		delete(w.vars, m[2])
		return true
	}
	w.vars[m[2]] = strings.Join(words, " ")
	return true
}

// local remembers a name's outer value the first time the current inlined
// call declares it local.
func (w *ksWalker) local(name string) {
	if len(w.locals) == 0 {
		return
	}
	frame := w.locals[len(w.locals)-1]
	if _, ok := frame[name]; ok {
		return
	}
	if v, ok := w.vars[name]; ok {
		frame[name] = &v
	} else {
		frame[name] = nil
	}
}

// inline runs a call to one of the script's functions, its arguments as
// $1 to $9 and its locals restored afterwards.
func (w *ksWalker) inline(fn string, l shLine, words []string) {
	if len(w.locals) >= ksMaxDepth {
		w.skip(fn, l, "calls nested too deep")
		return
	}
	args, ok := shWords(shExpand(strings.TrimPrefix(l.Text, words[0]), w.vars))
	if !ok {
		w.skip(fn, l, "an unbalanced quote")
		return
	}
	frame := map[string]*string{}
	w.locals = append(w.locals, frame)
	for i := 1; i <= 9; i++ {
		w.local(strconv.Itoa(i))
		if i <= len(args) {
			w.vars[strconv.Itoa(i)] = args[i-1]
		} else {
			delete(w.vars, strconv.Itoa(i))
		}
	}
	cond := w.cond
	w.cond = 0
	w.walk(words[0], w.funcs[words[0]])
	w.cond = cond
	w.locals = w.locals[:len(w.locals)-1]
	for name, v := range frame {
		if v == nil {
			delete(w.vars, name)
		} else {
			w.vars[name] = *v
		}
	}
	w.last = -1
}

var shSeqRE = regexp.MustCompile(`^\$\(seq (?:([0-9]+) )?([0-9]+)\)$`)

// forLoop runs `for v in LIST; do ... done` once per word of LIST and
// returns the index of its done.
func (w *ksWalker) forLoop(fn string, body []shLine, i int) int {
	head := body[i]
	end, depth := i+1, 1
	for ; end < len(body); end++ {
		t := body[end].Text
		first, _, _ := strings.Cut(t, " ")
		if first == "for" || first == "while" || first == "until" {
			depth++
		}
		if first == "done" || first == "done;" {
			if depth--; depth == 0 {
				break
			}
		}
	}
	start := i + 1
	if start < len(body) && body[start].Text == "do" {
		start++
	}
	inner := body[start:min(end, len(body))]
	m := regexp.MustCompile(`^for ([A-Za-z_][A-Za-z0-9_]*) in (.*?)(?:; ?do)?$`).FindStringSubmatch(head.Text)
	var items []string
	reason := ""
	switch {
	case w.cond > 0:
		reason = "under a condition"
	case m == nil || strings.HasSuffix(head.Text, "done"):
		reason = "a loop the miner does not expand"
	default:
		// `for i in a b;` with its do on the next line ends the list in ;.
		items, reason = w.loopItems(shExpand(strings.TrimSuffix(m[2], ";"), w.vars))
	}
	if reason != "" {
		w.skipIPLines(fn, inner, reason)
		return end
	}
	for _, it := range items {
		w.vars[m[1]] = it
		if w.walk(fn, inner) {
			break
		}
	}
	return end
}

// loopItems is a loop's words, or why they can't be known.
func (w *ksWalker) loopItems(list string) ([]string, string) {
	if s := shSeqRE.FindStringSubmatch(list); s != nil {
		if s[1] == "" {
			s[1] = "1" // seq N counts from 1
		}
		a, errA := strconv.Atoi(s[1])
		b, errB := strconv.Atoi(s[2])
		if errA != nil || errB != nil || b < a || b-a > 64 {
			return nil, "a seq the miner does not expand"
		}
		var items []string
		for n := a; n <= b; n++ {
			items = append(items, strconv.Itoa(n))
		}
		return items, ""
	}
	if strings.Contains(list, "$") || strings.Contains(list, "`") {
		return nil, "a loop over a computed list"
	}
	items, ok := shWords(list)
	if !ok {
		return nil, "an unbalanced quote"
	}
	return items, ""
}

// logTest attaches `log_test $? N "msg"` to the step the line before it made.
func (w *ksWalker) logTest(last int, text string) {
	args, ok := shWords(shExpand(strings.TrimPrefix(text, "log_test"), w.vars))
	if !ok || len(args) < 2 || args[0] != "$?" || last < 0 {
		return
	}
	n, err := strconv.Atoi(args[1])
	if err != nil {
		return
	}
	if w.steps[last].RC != nil && *w.steps[last].RC != n {
		return
	}
	w.steps[last].RC = &n
	if len(args) > 2 {
		w.steps[last].Msg = args[2]
	}
}

// runCmd handles `run_cmd "cmd"`: the selftest checks its rc, if at all,
// with the log_test after it, so it isn't required to succeed.
func (w *ksWalker) runCmd(fn string, l shLine) {
	arg := strings.TrimSpace(strings.TrimPrefix(l.Text, "run_cmd"))
	if len(arg) < 2 || (arg[0] != '"' && arg[0] != '\'') || arg[len(arg)-1] != arg[0] {
		w.skip(fn, l, "run_cmd without one quoted argument")
		return
	}
	inner := arg[1 : len(arg)-1]
	if arg[0] == '"' {
		inner = shExpand(inner, w.vars)
	}
	w.ipLine(fn, l, inner, false)
}

var shRedirRE = regexp.MustCompile(`\s+(?:&>>?|[0-9]*>>?|[0-9]*>&[0-9]+)\s*(?:/dev/null|&[0-9]+)?`)

// ksReaders are the commands a selftest pipes ip's output to when it
// checks the output rather than ip's rc.
var ksReaders = []string{"grep", "wc", "jq", "cut", "sed", "awk", "head", "tail", "sort", "uniq", "tr"}

// ipLine makes a step of one ip command, or a skip saying why not.
func (w *ksWalker) ipLine(fn string, l shLine, text string, bare bool) {
	strict := bare
	note := ""
	if before, after, ok := strings.Cut(text, " | "); ok && slices.Contains(ksReaders, firstWord(after)) &&
		!strings.Contains(after, "$(") {
		text, strict = before, false
		note = "the selftest checks its output with " + firstWord(after) + ", not its rc"
	}
	required := w.setE
	if before, _, ok := strings.Cut(text, " || "); ok {
		// cmd || true goes on under set -e too.
		text, strict, required = before, false, false
	}
	if shRedirRE.MatchString(text) {
		text, strict = shRedirRE.ReplaceAllString(text, ""), false
	}
	switch {
	case strings.Contains(text, "$(") || strings.Contains(text, "`"):
		w.skipWrite(fn, l, text, "a command substitution")
		return
	case strings.Contains(text, " | ") || strings.Contains(text, "&&") || strings.ContainsAny(text, ";<") ||
		strings.HasSuffix(text, "&"):
		w.skipWrite(fn, l, text, "a compound command")
		return
	}
	words, ok := shWords(shExpand(text, w.vars))
	if !ok || len(words) == 0 || words[0] != "ip" {
		if w.mentionsIP(text) {
			w.skip(fn, l, "not an ip command")
		}
		return
	}
	step, reason := w.ipStep(words)
	if reason != "" {
		if reason == ksOtherNS || strings.HasPrefix(reason, "ip netns") {
			w.skip(fn, l, reason)
		} else {
			w.skipWrite(fn, l, text, reason)
		}
		return
	}
	step.Line, step.Func = l.No, fn
	step.Strict = (strict || required) && !step.PreOnly
	if required && note == "" && !step.PreOnly {
		zero := 0
		step.RC, step.Msg = &zero, "required to succeed under set -e"
	}
	if w.lost > 0 && !step.PreOnly {
		// What the selftest states about this step assumes the skipped
		// write ran; here it didn't, so the step is neither required nor
		// checked.
		step.After, step.Strict, step.RC, step.Msg = w.lost, false, nil, ""
		note = strings.TrimPrefix(note+fmt.Sprintf("; it follows line %d, which the miner skipped", w.lost), "; ")
	}
	if note != "" {
		step.Note = strings.TrimPrefix(step.Note+"; "+note, "; ")
	}
	w.steps = append(w.steps, step)
	if note == "" {
		w.last = len(w.steps) - 1
	}
}

// ksOtherNS is the reason a command in another namespace is skipped.
const ksOtherNS = "runs in another namespace"

// ksWriteVerbs are the words that make a skipped ip command a write whose
// state later steps may need.
var ksWriteVerbs = []string{"add", "replace", "change", "append", "prepend", "del", "delete", "set", "flush"}

// skipWrite skips a line in the own namespace; if it is a write, every
// step after it loses what the selftest states about it.
func (w *ksWalker) skipWrite(fn string, l shLine, text, reason string) {
	w.skip(fn, l, reason)
	if w.lost > 0 {
		return
	}
	for _, word := range strings.Fields(text) {
		if slices.Contains(ksWriteVerbs, word) {
			w.lost = l.No
			return
		}
	}
}

// ownNS says whether a word names the namespace the tests run in.
func (w *ksWalker) ownNS(word string) bool {
	for _, v := range w.file.OwnNS {
		if word == "$"+v || word == "${"+v+"}" {
			return true
		}
	}
	return false
}

// ipStep is the step for an ip command's words, or why it has none.
func (w *ksWalker) ipStep(words []string) (ksStep, string) {
	out := []string{"ip"}
	i := 1
	for ; i < len(words) && strings.HasPrefix(words[i], "-"); i++ {
		switch words[i] {
		case "-netns", "-n":
			if i+1 >= len(words) {
				return ksStep{}, ksOtherNS
			}
			if !w.ownNS(words[i+1]) {
				return w.movedStep(words[i+1], words[i+2:])
			}
			i++
		case "-batch", "-b", "-force":
			return ksStep{}, "a batch file"
		default:
			out = append(out, words[i])
		}
	}
	if i >= len(words) {
		return ksStep{}, "no object"
	}
	obj := ipObject(words[i])
	if obj == "netns" {
		return w.netnsStep(words[i:])
	}
	var note string
	rest := words[i:]
	dev := ""
	for j := 0; j < len(rest); j++ {
		if rest[j] == "netns" && j+1 < len(rest) && obj == "link" {
			note = fmt.Sprintf("the selftest moves it to %s; here it stays, unaddressed", rest[j+1])
			if dev != "" {
				if w.moved == nil {
					w.moved = map[string]string{}
				}
				w.moved[dev] = rest[j+1]
			}
			j++
			continue
		}
		if j+1 < len(rest) && slices.Contains([]string{"add", "set", "dev", "name", "peer"}, rest[j]) &&
			rest[j+1] != "name" && rest[j+1] != "dev" {
			dev = rest[j+1]
		}
		out = append(out, rest[j])
	}
	for _, word := range out {
		if strings.Contains(word, "$") {
			return ksStep{}, "an unresolved variable " + word
		}
		if strings.ContainsAny(word, " \t'\"") || word == "" {
			return ksStep{}, "a quoted argument"
		}
	}
	return ksStep{Cmd: strings.Join(out, " "), Note: note}, ""
}

// movedStep keeps `ip -netns X link set [dev] D up|down` for a device D
// the selftest moved to X and the miner kept here; any other command in X
// is skipped.
func (w *ksWalker) movedStep(ns string, words []string) (ksStep, string) {
	if len(words) > 2 && words[2] == "dev" {
		words = slices.Delete(slices.Clone(words), 2, 3)
	}
	if len(words) != 4 || ipObject(words[0]) != "link" || words[1] != "set" || words[2] == "" ||
		w.moved[words[2]] != ns || (words[3] != "up" && words[3] != "down") {
		return ksStep{}, ksOtherNS
	}
	return ksStep{
		Cmd:  "ip link set " + words[2] + " " + words[3],
		Note: fmt.Sprintf("the selftest runs it in %s, where it moved %s", ns, words[2]),
	}, ""
}

// netnsStep keeps `ip netns exec <ours> sysctl ...` as a pre step; every
// other netns command is the workbench's to make.
func (w *ksWalker) netnsStep(words []string) (ksStep, string) {
	if len(words) >= 4 && words[1] == "exec" && w.ownNS(words[2]) && words[3] == "sysctl" {
		return ksStep{Cmd: strings.Join(words[3:], " "), PreOnly: true}, ""
	}
	if len(words) >= 3 && words[1] == "exec" && !w.ownNS(words[2]) {
		return ksStep{}, "runs in another namespace"
	}
	return ksStep{}, "ip netns: the workbench gives every row its own namespace"
}

// ipObjects is ip/ip.c's command table, in its order: ip takes the first
// object a word is a prefix of. It leaves out the table's British spelling of
// neighbor (ip/ip.c), which no prefix of "neighbor" reaches and no mined
// selftest types, so a word only that alias takes is skipped as unknown.
var ipObjects = []string{
	"address", "addrlabel", "maddress", "route", "rule", "neighbor", "ntable", "ntbl",
	"link", "l2tp", "fou", "ila", "macsec", "tunnel", "tunl", "tuntap", "tap", "token", "tcpmetrics",
	"tcp_metrics", "monitor", "xfrm", "mroute", "mrule", "netns", "netconf", "vrf", "sr", "nexthop",
	"mptcp", "ioam", "help", "stats",
}

// ipObject is the object ip runs for a word, or "" if none.
func ipObject(word string) string {
	for _, o := range ipObjects {
		if strings.HasPrefix(o, word) {
			return o
		}
	}
	return ""
}

// ksGroups lists a script's tests as functions, in the order its main loop
// runs them.
func ksGroups(f *ksFile, funcs map[string][]shLine, top []shLine, vars map[string]string) ([]string, error) {
	names, ok := shWords(vars[f.TestsVar])
	if !ok || len(names) == 0 {
		return nil, fmt.Errorf("%s: no tests in $%s", f.Name, f.TestsVar)
	}
	arms := map[string]string{}
	armRE := regexp.MustCompile(`^([A-Za-z0-9_|]+)\)\s*([A-Za-z0-9_]+);;$`)
	for _, l := range top {
		if m := armRE.FindStringSubmatch(l.Text); m != nil {
			for _, alt := range strings.Split(m[1], "|") {
				arms[alt] = m[2]
			}
		}
	}
	var groups []string
	for _, n := range names {
		fn := n
		if f.CaseMapped {
			if fn, ok = arms[n]; !ok {
				return nil, fmt.Errorf("%s: test %q has no case arm", f.Name, n)
			}
		}
		if _, ok := funcs[fn]; !ok {
			return nil, fmt.Errorf("%s: test %q names no function %s", f.Name, n, fn)
		}
		if !slices.Contains(groups, fn) {
			groups = append(groups, fn)
		}
	}
	return groups, nil
}

// mineScript makes one script's rows and skips.
func mineScript(f *ksFile, src string) ([]row, []ksSkip, error) {
	funcs, top := shFuncs(shLines(src))
	tw := &ksWalker{file: f, funcs: funcs, vars: map[string]string{}, last: -1}
	for _, l := range top {
		if !tw.condition(l.Text) && tw.cond == 0 {
			tw.assign(l.Text)
		}
	}
	groups, err := ksGroups(f, funcs, top, tw.vars)
	if err != nil {
		return nil, nil, err
	}
	var rows []row
	var skips []ksSkip
	for _, g := range groups {
		w := &ksWalker{file: f, funcs: funcs, vars: maps.Clone(tw.vars), last: -1}
		if _, ok := w.vars["IP"]; !ok {
			// A test's cleanup runs before its setup sets $IP; in the script
			// $IP is still the last test's, its namespace the same name.
			w.vars["IP"] = "ip -netns $" + f.OwnNS[0]
		}
		for _, p := range f.Prelude {
			w.walk(p, funcs[p])
			w.cond = 0
		}
		w.walk(g, funcs[g])
		rows = append(rows, ksRows(f, g, w.steps)...)
		for _, sk := range w.skips {
			// A prelude's or helper's line is skipped once per group that
			// runs it; it is listed once.
			if !slices.ContainsFunc(skips, func(o ksSkip) bool { return o.Line == sk.Line && o.Reason == sk.Reason }) {
				skips = append(skips, sk)
			}
		}
	}
	return rows, skips, nil
}

// ksRows makes a row of each route, rule and nexthop step of a group.
func ksRows(f *ksFile, group string, steps []ksStep) []row {
	var rows []row
	for k := range steps {
		s := &steps[k]
		words := strings.Fields(s.Cmd)
		obj := ""
		for _, word := range words[1:] {
			if !strings.HasPrefix(word, "-") {
				obj = ipObject(word)
				break
			}
		}
		if s.PreOnly || !slices.Contains(ksRowObjects, obj) || words[len(words)-1] == "help" {
			continue
		}
		r := row{
			ID:     fmt.Sprintf("%s/%s-%03d", f.Space, strings.ToLower(group), len(rows)+1),
			Desc:   ksDesc(f, group, s),
			Cmd:    s.Cmd,
			Pre:    ksPre(steps[:k]),
			Source: &source{Selftest: fmt.Sprintf("%s:%d %s", f.Name, s.Line, s.Func), Unchecked: s.Note},
		}
		if newer := ksNewerIn(s.Cmd); newer != "" {
			r.Desc = fmt.Sprintf("corner: %s, so the selftest's rc does not apply (%s %s): %s", newer, f.Name, group, s.Cmd)
			r.Source.Unchecked = strings.TrimPrefix(r.Source.Unchecked+"; "+newer, "; ")
		} else if s.RC != nil {
			rc := *s.RC
			r.Expect.RC = &rc
		}
		r.Modules = ksRowModules(steps[:k+1])
		rows = append(rows, r)
	}
	return rows
}

// ksDesc classes a row by the rc its log_test states, if any.
func ksDesc(f *ksFile, group string, s *ksStep) string {
	where := fmt.Sprintf("(%s %s)", f.Name, group)
	switch {
	case s.RC == nil:
		return fmt.Sprintf("corner: a step %s does not check: %s", where, s.Cmd)
	case *s.RC == 0:
		return fmt.Sprintf("positive: %s %s", s.Msg, where)
	default:
		return fmt.Sprintf("negative: %s %s", s.Msg, where)
	}
}

// ksPre is a row's pre steps: setup_ns's sysctls, then its group's steps
// before it. One the selftest doesn't require to succeed may fail as ip does.
func ksPre(steps []ksStep) []preStep {
	pre := make([]preStep, 0, 1+len(steps))
	pre = append(pre, preStep{Cmd: ksNsSetup})
	for i := range steps {
		p := preStep{Cmd: steps[i].Cmd}
		if !steps[i].Strict || ksNewerIn(steps[i].Cmd) != "" {
			p.OK = ksTolerated
		}
		pre = append(pre, p)
	}
	return pre
}

// ksRowModules is the modules for the link kinds a row's steps create.
func ksRowModules(steps []ksStep) []string {
	var mods []string
	for i := range steps {
		words := strings.Fields(steps[i].Cmd)
		for j := 0; j+1 < len(words); j++ {
			if m, ok := ksModules[words[j+1]]; ok && words[j] == "type" && !slices.Contains(mods, m) {
				mods = append(mods, m)
			}
		}
	}
	slices.Sort(mods)
	return mods
}

// mineKselftests mines every script of ksFiles from a selftests/net tree.
func mineKselftests(root fs.FS) ([]row, []ksSkip, error) {
	var rows []row
	var skips []ksSkip
	for i := range ksFiles {
		src, err := fs.ReadFile(root, ksFiles[i].Name)
		if err != nil {
			return nil, nil, err
		}
		r, s, err := mineScript(&ksFiles[i], string(src))
		if err != nil {
			return nil, nil, err
		}
		rows = append(rows, r...)
		skips = append(skips, s...)
	}
	return rows, skips, nil
}
