package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/emezav/markgrep/search"
)

func parse(body string) []search.Unit { return search.Parse("t.md", body) }

func mustSearch(t *testing.T, units []search.Unit, pats []string, o search.Options) []search.Match {
	t.Helper()
	ms, err := search.Search(units, pats, o)
	if err != nil {
		t.Fatalf("Search(%v): %v", pats, err)
	}
	return ms
}

// The equivalent must be RUNNABLE and must mean the same search, which is more
// than "a grep line appears". Both halves were false for fourteen versions and an
// audit found neither -- the test below named the invariant and only ever
// exercised the default path.
//
// -iF aborts with SIGABRT in a single-byte locale on at least one grep build,
// regardless of pattern or input, so a literal search printed a command that
// could not run. And a Go regexp handed to grep without -E is read as BRE, where
// alternation is literal: markgrep matched three units for `unit|line` and the
// command it printed matched one. ERE for both, with the literal escaped.
func TestPrintMatchesEmitsARunnableEquivalentThatMeansTheSameSearch(t *testing.T) {
	units := parse("something findable\n")

	for _, c := range []struct {
		name string
		pat  string
		o    search.Options
		want string
		root string
	}{
		{"literal", "findable", search.Options{Literal: true}, `grep -rniE 'findable' '.'`, "."},
		{"literal with metacharacters", "a.b*c", search.Options{Literal: true}, `grep -rniE 'a\.b\*c' '.'`, "."},
		{"regex keeps the pattern but declares ERE", "unit|line", search.Options{}, `grep -rniE 'unit|line' '.'`, "."},
		{"case sensitive drops the i", "findable", search.Options{CaseSens: true, Literal: true}, `grep -rnE 'findable' '.'`, "."},
		{"a root with a space is quoted too", "findable", search.Options{Literal: true}, `grep -rniE 'findable' 'with space'`, "with space"},
		{"a quote in the pattern survives", "it's", search.Options{Literal: true}, `grep -rniE 'it'\''s' '.'`, "."},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			ms := mustSearch(t, units, []string{"findable"}, search.Options{})
			PrintMatches(&buf, search.Result{Matches: ms}, []string{c.pat}, c.root, Output{}, c.o)
			if !strings.Contains(buf.String(), c.want) {
				t.Errorf("want the emitted command to contain\n  %s\ngot\n%s", c.want, buf.String())
			}
		})
	}
}

// -c and -l deliberately do NOT emit it: they are machine-readable output and a
// caller may be piping them. DECIDED 2026-08-10, when an audit found the
// invariant had been false for these two paths since the first commit and
// nothing had ever declared this. Pinned so the decision cannot drift back into
// being an accident.
func TestPrintMatchesKeepsMachineReadableOutputClean(t *testing.T) {
	units := append(search.Parse("a.md", "hit one\n"), search.Parse("b.md", "hit two\n")...)
	ms := mustSearch(t, units, []string{"hit"}, search.Options{})

	for _, c := range []struct {
		name string
		out  Output
	}{{"-c", Output{Count: true}}, {"-l", Output{FilesOnly: true}}} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintMatches(&buf, search.Result{Matches: ms}, []string{"hit"}, ".", c.out, search.Options{})
			if strings.Contains(buf.String(), "grep equivalent") {
				t.Errorf("%s must stay clean for a caller piping it, got\n%s", c.name, buf.String())
			}
		})
	}
}

func TestPrintMatchesCountAndFilesOnly(t *testing.T) {
	units := append(search.Parse("a.md", "hit one\n"), search.Parse("b.md", "hit two\n")...)
	ms := mustSearch(t, units, []string{"hit"}, search.Options{})

	var count bytes.Buffer
	PrintMatches(&count, search.Result{Matches: ms}, []string{"hit"}, ".", Output{Count: true}, search.Options{})
	if strings.TrimSpace(count.String()) != "2" {
		t.Errorf("-c should print just the count, got %q", count.String())
	}

	var files bytes.Buffer
	PrintMatches(&files, search.Result{Matches: ms}, []string{"hit"}, ".", Output{FilesOnly: true}, search.Options{})
	got := strings.Fields(files.String())
	if len(got) != 2 || got[0] != "a.md" || got[1] != "b.md" {
		t.Errorf("-l should print each path once, got %v", got)
	}
}

// An empty result whose pattern carries inline markup is the one zero this tool
// could not explain. Reported by stele, letter 113, after it cost them a real
// count: `--raw` shows the source with its asterisks, the engine matches against
// the flattened text without them, and nothing said so. Confirmed here and
// generalised -- `*`, backtick and `~` are absent from the searched text in all
// eight kinds, while the block marker survives, which is why `^-` finds three
// items and `^- \*` finds none.
//
// The note goes on the empty path only, and only in regex mode: under -F the
// pattern is normalized too, so the same search already works and telling the
// user to try -F would be wrong.
func TestEmptyResultExplainsInlineMarkupInThePattern(t *testing.T) {
	for _, c := range []struct {
		name string
		pat  string
		o    search.Options
		want bool
	}{
		{"asterisk in a regex", `^- \*item`, search.Options{}, true},
		{"backtick in a regex", "`code`", search.Options{}, true},
		{"tilde in a regex", "~del~", search.Options{}, true},
		{"a plain pattern says nothing extra", "absent", search.Options{}, false},
		{"literal mode already works, so no advice", "*item*", search.Options{Literal: true}, false},
		{"underscore survives, so it is not the cause", "_under_", search.Options{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintMatches(&buf, search.Result{}, []string{c.pat}, ".", Output{}, c.o)
			got := strings.Contains(buf.String(), "removed from the text")
			if got != c.want {
				t.Errorf("note present = %v, want %v, for %q:\n%s", got, c.want, c.pat, buf.String())
			}
		})
	}
}

// A non-empty result must not carry it, and neither must the machine-readable
// paths, which return before this block and stay clean by the decision of
// 2026-08-10.
func TestTheInlineMarkupNoteStaysOffEveryOtherPath(t *testing.T) {
	units := parse("something findable\n")
	ms := mustSearch(t, units, []string{"findable"}, search.Options{})

	for _, c := range []struct {
		name string
		ms   []search.Match
		out  Output
	}{
		{"a hit", ms, Output{}},
		{"-c", nil, Output{Count: true}},
		{"-l", nil, Output{FilesOnly: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			PrintMatches(&buf, search.Result{Matches: c.ms}, []string{`\*findable`}, ".", c.out, search.Options{})
			if strings.Contains(buf.String(), "removed from the text") {
				t.Errorf("%s must not carry the note:\n%s", c.name, buf.String())
			}
		})
	}
}

func TestPrintMatchesSaysSoWhenThereAreNone(t *testing.T) {
	var buf bytes.Buffer
	PrintMatches(&buf, search.Result{}, []string{"absent"}, "docs/", Output{}, search.Options{})
	if !strings.Contains(buf.String(), "no matches") {
		t.Errorf("an empty result must say so explicitly:\n%s", buf.String())
	}
}
