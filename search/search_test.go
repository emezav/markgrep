package search

import (
	"strings"
	"testing"
)

func parse(body string) []Unit { return Parse("t.md", body) }

func mustSearch(t *testing.T, units []Unit, pats []string, o Options) []Match {
	t.Helper()
	ms, err := Search(units, pats, o)
	if err != nil {
		t.Fatalf("Search(%v): %v", pats, err)
	}
	return ms
}

func TestSearchIsCaseInsensitiveByDefault(t *testing.T) {
	units := parse("The Rule Of Thumb applies here.\n")
	if got := len(mustSearch(t, units, []string{"rule of thumb"}, Options{})); got != 1 {
		t.Errorf("default search should be case insensitive, got %d matches", got)
	}
	if got := len(mustSearch(t, units, []string{"rule of thumb"}, Options{CaseSens: true})); got != 0 {
		t.Errorf("-s should be case sensitive, got %d matches", got)
	}
}

// The asymmetry that cost a bug: markup is stripped from the TEXT always, but
// from the PATTERN only in literal mode, because normalizing a regular
// expression eats its operators.
func TestNormalizeAppliesToThePatternOnlyInLiteralMode(t *testing.T) {
	units := parse("**Rule of thumb:** do not read a whole file.\n")

	if got := len(mustSearch(t, units, []string{"**Rule of thumb:**"}, Options{Literal: true})); got != 1 {
		t.Errorf("-F should normalize the pattern and match, got %d", got)
	}
	if got := len(mustSearch(t, units, []string{"rule.*thumb"}, Options{})); got != 1 {
		t.Errorf("regex operators must survive, got %d", got)
	}
	if got := len(mustSearch(t, units, []string{"rule.*thumb"}, Options{Literal: true})); got != 0 {
		t.Errorf("-F must treat the pattern as a fixed string, got %d", got)
	}
}

func TestInvertReturnsUnitsWhereNoPhrasingMatches(t *testing.T) {
	units := parse("alpha here\n\nbeta here\n")
	ms := mustSearch(t, units, []string{"alpha"}, Options{Invert: true})
	if len(ms) != 1 {
		t.Fatalf("want 1 inverted match, got %d", len(ms))
	}
	if !strings.Contains(ms[0].U.Flat(), "beta") {
		t.Errorf("inverted match returned the wrong unit: %q", ms[0].U.Flat())
	}
}

func TestSeveralPhrasingsAreGroupedNotCounted(t *testing.T) {
	units := parse("one idea said twice, idea and idea again.\n")
	ms := mustSearch(t, units, []string{"one idea", "idea again"}, Options{})
	if len(ms) != 1 {
		t.Fatalf("two phrasings hitting one unit must yield ONE match, got %d", len(ms))
	}
	if len(ms[0].Patterns) != 2 {
		t.Errorf("both phrasings should be recorded as hits, got %v", ms[0].Patterns)
	}
}

func TestKindFilter(t *testing.T) {
	units := parse("a paragraph with text\n\n- an item with text\n")
	if got := len(mustSearch(t, units, []string{"with text"}, Options{Kind: "any"})); got != 2 {
		t.Errorf("kind any should match both, got %d", got)
	}
	if got := len(mustSearch(t, units, []string{"with text"}, Options{Kind: "item"})); got != 1 {
		t.Errorf("kind item should match one, got %d", got)
	}
}

func TestMatchesAreSortedByPathThenLine(t *testing.T) {
	units := append(Parse("b.md", "hit here\n"), Parse("a.md", "hit here\n\nhit again here\n")...)
	ms := mustSearch(t, units, []string{"hit"}, Options{})
	var order []string
	for _, m := range ms {
		order = append(order, m.U.Path)
	}
	if len(order) != 3 || order[0] != "a.md" || order[1] != "a.md" || order[2] != "b.md" {
		t.Errorf("matches not sorted by path then line: %v", order)
	}
}

func TestCompileRejectsABadPattern(t *testing.T) {
	if _, err := Search(parse("x\n"), []string{"("}, Options{}); err == nil {
		t.Error("an invalid regular expression must return an error, not match nothing")
	}
}
