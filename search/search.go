// Package search is markgrep's unit model and matcher, importable so that a
// caller can search markdown prose by semantic unit without going through a
// shell.
//
// It exists because stele-mcp asked for it in their letter 005 (2026-11-01):
// they wanted a search tool over MCP and found theirs was a worse duplicate of
// this one, and a pattern that reaches a tool through a shell is the text most
// likely to lose a backslash on the way. The CLI is a thin layer over Run and
// its output did not change when this package was split out.
//
// The contract is Run, Options and Result. The rest is exported because the
// CLI and the tests use it, and it carries the same comments it had in main.
package search

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// shellQuote wraps a pattern so it survives a shell verbatim. It exists because
// %q is GO quoting, not shell quoting: it doubles a backslash, so the escaped
// literal `a\.b` was printed as "a\\.b" and pasting it searched for something
// else. Single quotes make every byte literal, which is what a pattern needs,
// and an embedded single quote is closed, escaped and reopened -- the one form
// POSIX sh gives no shorthand for.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Match is a unit where at least one of the phrasings hit.
type Match struct {
	U        Unit
	Patterns []string
}

// Options carries what the user asked for. Flags borrowed from grep do what
// they do in grep, TRANSLATED to the unit model: where grep says "line", read
// "unit". The ones that cannot be translated are rejected with an explanation
// instead of half-implemented.
//
// Only what changes WHICH units come back lives here. How they are printed --
// -c, -l, --raw -- belongs to the CLI, and stayed there when this became a
// package.
type Options struct {
	Literal  bool   // -F: fixed string, not a regular expression
	CaseSens bool   // -s: case sensitive (default is insensitive; see help)
	Invert   bool   // -v: units where NO phrasing hits
	Kind     string // comma-separated kinds; empty or "any" lets everything through
	Links    string // text | url | both; empty means text

	// Include and Exclude are base-name globs and ACCUMULATE, as in grep: a file
	// passes if it matches any Include (or there is none) and no Exclude. Until
	// 0.12.0 each was one string and a second flag replaced the first without a
	// word, so `--exclude a --exclude b` excluded only b. Found by stele-mcp.
	Include []string
	Exclude []string

	// Control holds phrases that must each match at least one unit in the same
	// corpus with the same options. If one matches nothing the search fails with
	// ErrControlFailed and reports no matches, because a search that cannot find
	// a phrase known to be there cannot be trusted to say anything is absent.
	// -v does not apply to it: the control asks whether the corpus answers.
	Control []string
}

// ErrFilterMatchesNothing is returned when an --include or --exclude glob
// matches none of the files collected, or a --kind none of the units left. A
// filter that filters nothing reads exactly like one that worked: the only
// trace was the corpus count, equal to that of a search without the filter,
// which nobody has in front of them.
var ErrFilterMatchesNothing = errors.New("filter matches nothing")

// ErrEmptyCorpus is returned when no unit is left to search: a root with no
// markdown, an empty file, or filters that each match something and together
// leave nothing. Zero matches over zero units read exactly like a clean corpus,
// and the CLI exited 1 for it. Found by stele-mcp's sweep, letter 009.
var ErrEmptyCorpus = errors.New("nothing to search")

// ErrControlFailed is returned when a Control phrase matches no unit.
var ErrControlFailed = errors.New("positive control matched nothing")

// kindAllowed reports whether a unit kind passes the --kind spec, which is a
// comma-separated list. Empty or "any" lets everything through.
func kindAllowed(kind, spec string) bool {
	if spec == "" || spec == "any" {
		return true
	}
	for _, k := range strings.Split(spec, ",") {
		if strings.TrimSpace(k) == kind {
			return true
		}
	}
	return false
}

// anyKind reports whether at least one unit passes the --kind spec.
func anyKind(units []Unit, spec string) bool {
	for _, u := range units {
		if kindAllowed(u.Kind, spec) {
			return true
		}
	}
	return false
}

// CheckKinds validates a --kind spec against the known kinds.
//
// The alternative is worse than it looks: an unknown name filters every unit
// out and the run reports zero matches, which is indistinguishable from a
// corpus that genuinely does not contain the phrase. That is the exact failure
// this tool exists to remove, so it must not be the way a typo behaves.
func CheckKinds(spec string) error {
	if spec == "" || spec == "any" {
		return nil
	}
	for _, k := range strings.Split(spec, ",") {
		k = strings.TrimSpace(k)
		known := k == "any"
		for _, valid := range Kinds {
			if k == valid {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("unknown --kind %q; known kinds: %s, any",
				k, strings.Join(Kinds, ", "))
		}
	}
	return nil
}

// CheckLinks validates a --links mode.
func CheckLinks(mode string) error {
	switch mode {
	case "", LinksText, LinksURL, LinksBoth:
		return nil
	}
	return fmt.Errorf("unknown --links %q; use %s, %s or %s", mode, LinksText, LinksURL, LinksBoth)
}

// checkGlobs refuses a malformed glob before anything is read. filepath.Match
// reports ErrBadPattern and the filter used to drop it on the floor, so `[`
// matched nothing: as --include it emptied the corpus, as --exclude it
// excluded nothing, and neither said why.
func checkGlobs(o Options) error {
	for _, g := range append(append([]string{}, o.Include...), o.Exclude...) {
		if _, err := filepath.Match(g, ""); err != nil {
			return fmt.Errorf("glob %q: %w", g, err)
		}
	}
	return nil
}

// Filter applies the Include and Exclude globs to the file base names, and
// fails if any glob matches none of the files it was given -- including an
// --include, which used to empty the corpus and report "0 files". That one was
// visible; with several includes accumulating, a dead one among live ones is
// not, so every glob is held to the same rule.
func Filter(units []Unit, files []string, o Options) ([]Unit, []string, error) {
	if len(o.Include) == 0 && len(o.Exclude) == 0 {
		return units, files, nil
	}
	if err := checkGlobs(o); err != nil {
		return nil, nil, err
	}
	matches := func(globs []string, path string) bool {
		base := filepath.Base(path)
		for _, g := range globs {
			if m, _ := filepath.Match(g, base); m {
				return true
			}
		}
		return false
	}
	for _, set := range []struct {
		flag  string
		globs []string
	}{{"--include", o.Include}, {"--exclude", o.Exclude}} {
		for _, g := range set.globs {
			if !matchesAny(g, files) {
				return nil, nil, fmt.Errorf("%s %q matches none of the %d files collected: %w",
					set.flag, g, len(files), ErrFilterMatchesNothing)
			}
		}
	}
	ok := func(path string) bool {
		if len(o.Include) > 0 && !matches(o.Include, path) {
			return false
		}
		return !matches(o.Exclude, path)
	}
	var uu []Unit
	for _, x := range units {
		if ok(x.Path) {
			uu = append(uu, x)
		}
	}
	var ff []string
	for _, x := range files {
		if ok(x) {
			ff = append(ff, x)
		}
	}
	return uu, ff, nil
}

func matchesAny(glob string, files []string) bool {
	for _, f := range files {
		if m, _ := filepath.Match(glob, filepath.Base(f)); m {
			return true
		}
	}
	return false
}

// compile turns phrasings into expressions.
//
// Mind the asymmetry, it cost a bug: markup is stripped from the TEXT always,
// but from the PATTERN only in literal mode. Normalizing a regular expression
// eats its operators -- "rule.*thumb" became "rule.thumb" and stopped matching
// anything. In regex mode the pattern is used as written, and the user writes
// it WITHOUT markup, which is what the text no longer has.
//
// Normalizing the text and normalizing the pattern look like the same
// operation, and stop being it the moment the pattern is an expression rather
// than a string.
func compile(patterns []string, o Options) ([]*regexp.Regexp, error) {
	var out []*regexp.Regexp
	for _, p := range patterns {
		expr := p
		if o.Literal {
			expr = regexp.QuoteMeta(NormalizeWith(p, o.Links))
		}
		if !o.CaseSens {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Search returns the units where at least one phrasing hits.
//
// Several patterns mean several ways of saying the SAME thing: they are grouped
// on purpose, because the question being answered is "where does this idea
// live", not "how many times does this string occur".
func Search(units []Unit, patterns []string, o Options) ([]Match, error) {
	res, err := compile(patterns, o)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, u := range units {
		if !kindAllowed(u.Kind, o.Kind) {
			continue
		}
		flat := u.FlatWith(o.Links)
		var hits []string
		for i, re := range res {
			if re.MatchString(flat) {
				hits = append(hits, patterns[i])
			}
		}
		if o.Invert {
			if len(hits) == 0 {
				out = append(out, Match{u, nil})
			}
			continue
		}
		if len(hits) > 0 {
			out = append(out, Match{u, hits})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].U.Path != out[j].U.Path {
			return out[i].U.Path < out[j].U.Path
		}
		return out[i].U.Line < out[j].U.Line
	})
	return out, nil
}

// GrepEquivalent returns the grep command that reproduces the search, one per
// phrasing.
//
// Invariant: the fallback is not theoretical. Whoever lacks this tool can
// reproduce the search worse but reproduce it -- and whoever has it can drop
// it without losing anything. "Reproduce" has two halves and both were false
// for fourteen versions, which is why the flags below look the way they do.
//
// RUNNABLE. `-i` together with `-F` aborts with SIGABRT in a single-byte
// locale (C, POSIX, or none set) on at least one GNU grep 3.0 build. It is
// not the data: a pure-ASCII file, an ASCII pattern, an empty pattern and
// stdin all abort alike, and the same call under any UTF-8 locale works. So a
// literal search used to print a command that could not run at all wherever
// the caller had no locale set -- which is every cron job and every minimal
// container.
//
// THE SAME SEARCH. A Go regexp handed to grep with no dialect flag is read as
// BRE, where alternation is a literal character: for `unit|line` markgrep
// matched three units and the command it printed matched one. That is not a
// weaker reproduction, it is a different question.
//
// ERE fixes both, and a literal is escaped into it rather than declared with
// -F. RE2 is far closer to ERE than to BRE, so this is also the honest
// translation of what actually ran.
//
// The file filters travel too, since 0.12.0: until then a search with
// --exclude printed a command over the whole tree, the same half of the
// invariant one level over.
func GrepEquivalent(patterns []string, root string, o Options) []string {
	flags := "-rn"
	if !o.CaseSens {
		flags += "i"
	}
	flags += "E"
	if o.Invert {
		flags += "v"
	}
	var filters string
	for _, g := range o.Include {
		filters += " --include=" + shellQuote(g)
	}
	for _, g := range o.Exclude {
		filters += " --exclude=" + shellQuote(g)
	}
	var out []string
	for _, p := range patterns {
		if o.Literal {
			p = regexp.QuoteMeta(p)
		}
		// The root is quoted too: a directory with a space in it used to print as
		// two arguments, which is the same half of the invariant one level over.
		out = append(out, fmt.Sprintf("grep %s%s %s %s", flags, filters, shellQuote(p), shellQuote(root)))
	}
	return out
}

// Result is what a search returns: the corpus it actually ran over, the units
// that matched, and the grep command that reproduces it.
type Result struct {
	Files   []string // files searched, AFTER the filters; slash-separated
	Units   int      // units in those files, before --kind
	Matches []Match
	Grep    []string // one equivalent grep command per phrasing
}

// Run is the whole search the CLI performs: validate, collect, filter, run the
// controls, match. Every refusal is an error, never an empty Result, because an
// empty Result is the one answer this tool must not give by accident.
//
// On ErrControlFailed, ErrEmptyCorpus and a --kind that matched nothing the
// Result still carries Files and Units, so the caller can say which corpus
// failed to answer; Matches is nil.
func Run(root string, patterns []string, o Options) (Result, error) {
	if len(patterns) == 0 {
		return Result{}, errors.New("at least one pattern is required (-e/-f)")
	}
	if err := CheckKinds(o.Kind); err != nil {
		return Result{}, err
	}
	if err := CheckLinks(o.Links); err != nil {
		return Result{}, err
	}
	if err := checkGlobs(o); err != nil {
		return Result{}, err
	}
	if _, err := compile(append(append([]string{}, patterns...), o.Control...), o); err != nil {
		return Result{}, err
	}
	units, files, err := Collect(root)
	if err != nil {
		return Result{}, fmt.Errorf("read error: %w", err)
	}
	units, files, err = Filter(units, files, o)
	if err != nil {
		return Result{}, err
	}
	r := Result{Files: files, Units: len(units), Grep: GrepEquivalent(patterns, root, o)}

	// The criterion is units, not files: an empty .md is one file and nothing
	// to search, and it exited 1 like any honest miss.
	if len(units) == 0 {
		return r, fmt.Errorf("%d files, 0 units under %s after the filters: %w", len(files), root, ErrEmptyCorpus)
	}
	// Same shape as a dead --include, one grain down: --kind table over a
	// corpus with no table printed "1 units" (counted before --kind) and exit 1.
	if !anyKind(units, o.Kind) {
		return r, fmt.Errorf("--kind %q matches none of the %d units in %d files: %w",
			o.Kind, len(units), len(files), ErrFilterMatchesNothing)
	}

	co := o
	co.Invert = false
	for _, c := range o.Control {
		ms, err := Search(units, []string{c}, co)
		if err != nil {
			return r, err
		}
		if len(ms) == 0 {
			return r, fmt.Errorf("--control %q matched 0 of %d units in %d files with the same options; "+
				"nothing is reported: %w", c, len(units), len(files), ErrControlFailed)
		}
	}

	r.Matches, err = Search(units, patterns, o)
	if err != nil {
		return Result{}, err
	}
	return r, nil
}
