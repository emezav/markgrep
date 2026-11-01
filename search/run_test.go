package search

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// corpus writes three files, the phrase in two of them, which is the smallest
// tree where "excluded one of two" and "excluded both" give different answers.
// Same shape as stele-mcp's probe over their bitacora/, letter 005.
func corpus(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	write(t, filepath.Join(d, "memory.md"), "# A\n\nThe integrated searcher lives here.\n")
	write(t, filepath.Join(d, "latest.md"), "# B\n\nThe integrated searcher also here.\n")
	write(t, filepath.Join(d, "other.md"), "# C\n\n- an item about nothing\n")
	return d
}

func paths(ms []Match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, filepath.Base(m.U.Path))
	}
	return out
}

func mustRun(t *testing.T, root string, o Options) Result {
	t.Helper()
	r, err := Run(root, []string{"integrated searcher"}, o)
	if err != nil {
		t.Fatalf("Run(%+v): %v", o, err)
	}
	return r
}

// The defect stele-mcp found: a second --exclude replaced the first, so asking
// for two files out returned a hit from the one asked out first. The baseline
// row is the control -- without it, "1 hit" could mean either answer.
func TestFiltersAccumulateAsInGrep(t *testing.T) {
	d := corpus(t)
	for _, c := range []struct {
		name string
		o    Options
		want []string
	}{
		{"no filter (control)", Options{}, []string{"latest.md", "memory.md"}},
		{"one exclude", Options{Exclude: []string{"memory.md"}}, []string{"latest.md"}},
		{"two excludes drop both", Options{Exclude: []string{"memory.md", "latest.md"}}, nil},
		{"two includes keep either", Options{Include: []string{"memory.md", "latest.md"}}, []string{"latest.md", "memory.md"}},
		{"include and exclude together", Options{Include: []string{"*.md"}, Exclude: []string{"latest.md"}}, []string{"memory.md"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			eq(t, paths(mustRun(t, d, c.o).Matches), c.want)
		})
	}
}

// A filter that filters nothing reads exactly like one that worked. The include
// case used to be visible -- "0 files" -- but with includes accumulating, a dead
// one beside a live one is not, so both directions are held to the same rule.
func TestAFilterThatMatchesNoFileIsAnError(t *testing.T) {
	d := corpus(t)
	for _, c := range []struct {
		name string
		o    Options
		dead bool
	}{
		{"dead exclude", Options{Exclude: []string{"noexiste*.md"}}, true},
		{"dead include", Options{Include: []string{"noexiste*.md"}}, true},
		{"dead include beside a live one", Options{Include: []string{"memory.md", "noexiste*.md"}}, true},
		{"dead exclude beside a live one", Options{Exclude: []string{"memory.md", "noexiste*.md"}}, true},
		{"live globs (control)", Options{Include: []string{"*.md"}, Exclude: []string{"other.md"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Run(d, []string{"integrated searcher"}, c.o)
			if got := errors.Is(err, ErrFilterMatchesNothing); got != c.dead {
				t.Errorf("dead filter reported = %v, want %v (err: %v)", got, c.dead, err)
			}
			if c.dead && !strings.Contains(err.Error(), "noexiste*.md") {
				t.Errorf("the error must name the glob that did nothing: %v", err)
			}
		})
	}
}

// filepath.Match's ErrBadPattern used to be dropped on the floor: `[` as an
// --exclude excluded nothing and exited 0. It is refused before anything is
// read, so it fails the same way on a corpus that does not exist.
func TestAMalformedGlobIsAnError(t *testing.T) {
	d := corpus(t)
	for _, o := range []Options{{Exclude: []string{"["}}, {Include: []string{"["}}} {
		_, err := Run(d, []string{"integrated searcher"}, o)
		if !errors.Is(err, filepath.ErrBadPattern) {
			t.Errorf("%+v: want ErrBadPattern, got %v", o, err)
		}
	}
	if _, err := Run(filepath.Join(d, "missing"), []string{"x"}, Options{Exclude: []string{"["}}); !errors.Is(err, filepath.ErrBadPattern) {
		t.Errorf("the glob must be refused before the corpus is read, got %v", err)
	}
}

// A positive control is what caught the zero over bitacora/ in stele's
// verificar.md, not the tool. It must fail the whole search when it finds
// nothing, report no matches, and still say which corpus did not answer.
func TestAFailedControlFailsTheSearch(t *testing.T) {
	d := corpus(t)

	r := mustRun(t, d, Options{Control: []string{"lives here"}})
	if len(r.Matches) != 2 {
		t.Fatalf("a passing control must not change the result, got %v", paths(r.Matches))
	}

	r, err := Run(d, []string{"integrated searcher"}, Options{Control: []string{"lives here", "zzqq absent"}})
	if !errors.Is(err, ErrControlFailed) {
		t.Fatalf("want ErrControlFailed, got %v", err)
	}
	if r.Matches != nil {
		t.Errorf("a failed control must report no matches, got %v", paths(r.Matches))
	}
	if len(r.Files) != 3 || r.Units == 0 {
		t.Errorf("a failed control must still say which corpus failed: %d files, %d units", len(r.Files), r.Units)
	}
	if !strings.Contains(err.Error(), "zzqq absent") {
		t.Errorf("the error must name the control that failed: %v", err)
	}
}

// "With the same options" is the point: a control that runs with looser options
// than the search proves the corpus answers a question nobody asked. --kind and
// the filters apply to it; -v does not, because inverted it would pass on any
// corpus that has one unit without the phrase.
func TestTheControlRunsWithTheSameOptions(t *testing.T) {
	d := corpus(t)
	for _, c := range []struct {
		name string
		o    Options
		fail bool
	}{
		{"--kind applies: the control is a paragraph", Options{Kind: "item", Control: []string{"lives here"}}, true},
		{"--kind applies (control): an item control under --kind item", Options{Kind: "item", Control: []string{"about nothing"}}, false},
		{"filters apply: its file is excluded", Options{Exclude: []string{"memory.md"}, Control: []string{"lives here"}}, true},
		{"-v does not invert it", Options{Invert: true, Control: []string{"zzqq absent"}}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Run(d, []string{"integrated searcher"}, c.o)
			if got := errors.Is(err, ErrControlFailed); got != c.fail {
				t.Errorf("control failed = %v, want %v (err: %v)", got, c.fail, err)
			}
		})
	}
}

// The equivalent searched the whole tree when the search had been filtered,
// which is a different question printed as the same one.
func TestGrepEquivalentCarriesTheFilters(t *testing.T) {
	got := GrepEquivalent([]string{"x"}, ".", Options{Include: []string{"*.md"}, Exclude: []string{"a b.md", "c.md"}})
	want := `grep -rniE --include='*.md' --exclude='a b.md' --exclude='c.md' 'x' '.'`
	if len(got) != 1 || got[0] != want {
		t.Errorf("want\n  %s\ngot\n  %v", want, got)
	}
	if g := GrepEquivalent([]string{"x"}, ".", Options{}); g[0] != `grep -rniE 'x' '.'` {
		t.Errorf("without filters the command must not change: %s", g[0])
	}
}

// stele-mcp's sweep, letter 009: filters that each match something and together
// leave nothing, and a root with no markdown, returned an empty Result and a nil
// error. The empty .md is ours: one file and zero units, so the criterion is
// units, not files. The control row is the same corpus with one live filter.
func TestNothingToSearchIsAnError(t *testing.T) {
	d := corpus(t)
	nomd := t.TempDir()
	write(t, filepath.Join(nomd, "notes.txt"), "integrated searcher\n")
	empty := t.TempDir()
	write(t, filepath.Join(empty, "empty.md"), "")
	for _, c := range []struct {
		name  string
		root  string
		o     Options
		empty bool
	}{
		{"include and exclude cancel", d, Options{Include: []string{"latest.md"}, Exclude: []string{"latest.md"}}, true},
		{"excludes drop every file", d, Options{Exclude: []string{"memory.md", "latest.md", "other.md"}}, true},
		{"no markdown under the root", nomd, Options{}, true},
		{"an empty markdown file", empty, Options{}, true},
		{"live filter (control)", d, Options{Include: []string{"latest.md"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, err := Run(c.root, []string{"integrated searcher"}, c.o)
			if got := errors.Is(err, ErrEmptyCorpus); got != c.empty {
				t.Errorf("empty corpus reported = %v, want %v (err: %v)", got, c.empty, err)
			}
			if c.empty && r.Matches != nil {
				t.Errorf("an empty corpus must report no matches, got %v", paths(r.Matches))
			}
		})
	}
}

// --kind table over a corpus with no table printed "1 units" -- counted before
// --kind -- and exited 1: a dead filter one grain down from a dead --include.
func TestAKindThatMatchesNoUnitIsAnError(t *testing.T) {
	d := corpus(t)
	for _, c := range []struct {
		name string
		o    Options
		dead bool
	}{
		{"no table in the corpus", Options{Kind: "table"}, true},
		{"its only item is excluded", Options{Kind: "item", Exclude: []string{"other.md"}}, true},
		{"a live kind (control)", Options{Kind: "item"}, false},
		{"a list with one live kind (control)", Options{Kind: "table,paragraph"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Run(d, []string{"integrated searcher"}, c.o)
			if got := errors.Is(err, ErrFilterMatchesNothing); got != c.dead {
				t.Errorf("dead --kind reported = %v, want %v (err: %v)", got, c.dead, err)
			}
			if c.dead && !strings.Contains(err.Error(), c.o.Kind) {
				t.Errorf("the error must name the kind that did nothing: %v", err)
			}
		})
	}
}

// Every refusal is an error and never an empty Result, because an empty Result
// is the one answer that must not happen by accident.
func TestRunRefusesInsteadOfReturningEmpty(t *testing.T) {
	d := corpus(t)
	for _, c := range []struct {
		name string
		pats []string
		o    Options
	}{
		{"no pattern", nil, Options{}},
		{"unknown kind", []string{"x"}, Options{Kind: "paragrph"}},
		{"unknown links", []string{"x"}, Options{Links: "texts"}},
		{"bad regex", []string{"("}, Options{}},
		{"bad control regex", []string{"x"}, Options{Control: []string{"("}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Run(d, c.pats, c.o); err == nil {
				t.Error("want an error, got none")
			}
		})
	}
}
