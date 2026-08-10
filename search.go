package main

import (
	"fmt"
	"io"
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
type Options struct {
	Literal   bool // -F: fixed string, not a regular expression
	CaseSens  bool // -s: case sensitive (default is insensitive; see help)
	Invert    bool // -v: units where NO phrasing hits
	FilesOnly bool // -l: paths only
	Count     bool // -c: count only
	Kind      string
	Include   string
	Exclude   string
	Links     string // text | url | both; empty means text
}

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

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// PrintMatches writes the result: the whole unit and the section it lives in.
// That is the only thing this tool adds over grep, and the reason it exists.
//
// It never emits a verdict. It says what is there and where it lives.
func PrintMatches(w io.Writer, ms []Match, patterns []string, root string, raw bool, o Options) {
	if o.Count {
		fmt.Fprintf(w, "%d\n", len(ms))
		return
	}
	if o.FilesOnly {
		seen := map[string]bool{}
		for _, m := range ms {
			if !seen[m.U.Path] {
				seen[m.U.Path] = true
				fmt.Fprintln(w, m.U.Path)
			}
		}
		return
	}

	if len(ms) == 0 {
		fmt.Fprintf(w, "no matches for %d phrasing(s) in %s\n", len(patterns), root)
	}
	for _, m := range ms {
		h := m.U.Heading
		if h == "" {
			h = "(no section)"
		}
		fmt.Fprintf(w, "\n%s:%d  [%s]  %s\n", m.U.Path, m.U.Line, m.U.Kind, h)
		if len(m.Patterns) > 0 {
			fmt.Fprintf(w, "  hit: %s\n", strings.Join(m.Patterns, " | "))
		}
		// A nested item alone can read as nonsense: it qualifies a claim that is
		// not in the unit. Every unit already carries its nearest heading for the
		// same reason, and this is that reason one level down.
		if m.U.Parent != "" {
			fmt.Fprintf(w, "  under: %s\n", clip(m.U.Parent, 200))
		}
		text := m.U.Raw()
		if !raw {
			text = clip(m.U.FlatWith(o.Links), 600)
		}
		for _, l := range strings.Split(text, "\n") {
			fmt.Fprintf(w, "  | %s\n", l)
		}
	}

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
	// -c and -l return before reaching here, deliberately: they are
	// machine-readable output and a caller may be piping them. DECIDED
	// 2026-08-10, when an audit found the invariant had been false for those two
	// paths since the first commit and nothing had ever declared it. Pinned by
	// TestPrintMatchesKeepsMachineReadableOutputClean.
	fmt.Fprintf(w, "\n--- grep equivalent (worse: line-based, misses anything the wrap split) ---\n")
	flags := "-rn"
	if !o.CaseSens {
		flags += "i"
	}
	flags += "E"
	if o.Invert {
		flags += "v"
	}
	for _, p := range patterns {
		if o.Literal {
			p = regexp.QuoteMeta(p)
		}
		// The root is quoted too: a directory with a space in it used to print as
		// two arguments, which is the same half of the invariant one level over.
		fmt.Fprintf(w, "  grep %s %s %s\n", flags, shellQuote(p), shellQuote(root))
	}
	fmt.Fprintf(w, "\nmatching units: %d\n", len(ms))
}
