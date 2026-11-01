package search

import (
	"strings"
	"testing"
)

// --- inline HTML -----------------------------------------------------------

// The third construct, after emphasis and links, that splits a phrase nobody
// wrote as split.
func TestInlineTagsDoNotSplitAPhrase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"an <b>important</b> idea", "an important idea"},
		{"a <span class=\"x\">marked</span> span", "a marked span"},
		{"line one<br/>line two", "line one line two"},
		{"an <em>idea</em>, punctuated", "an idea, punctuated"},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A bare angle bracket in prose is not a tag and must survive.
func TestBareAngleBracketsSurvive(t *testing.T) {
	cases := []string{
		"a < b and c > d",
		"the < operator",
		"5 <= 6",
	}
	for _, in := range cases {
		if got := Normalize(in); got != in {
			t.Errorf("Normalize(%q) mangled prose into %q", in, got)
		}
	}
}

func TestAutolinksStillWinOverTagStripping(t *testing.T) {
	if got, want := Normalize("see <https://example.com/a> now"), "see https://example.com/a now"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- HTML blocks -----------------------------------------------------------

func TestHTMLBlockIsItsOwnKind(t *testing.T) {
	units := Parse("t.md", "text before\n\n<div class=\"note\">\n  inner content\n</div>\n\ntext after\n")
	eq(t, kindsOf(units), []string{"paragraph@1", "html@3", "paragraph@7"})
}

func TestHTMLCommentIsAnHTMLBlockEvenAcrossLines(t *testing.T) {
	units := Parse("t.md", "before\n\n<!-- a note\n     spanning lines -->\n\nafter\n")
	eq(t, kindsOf(units), []string{"paragraph@1", "html@3", "paragraph@6"})
}

// The failure mode frontMatter already guards against: an unterminated block
// must not swallow the rest of the document.
func TestUnterminatedHTMLCommentDoesNotSwallowTheDocument(t *testing.T) {
	units := Parse("t.md", "before\n\n<!-- never closed\n\nafter\n\nmore after\n")
	var paragraphs int
	for _, u := range units {
		if u.Kind == "paragraph" {
			paragraphs++
		}
	}
	if paragraphs < 2 {
		t.Fatalf("an unterminated comment ate the document: %v", kindsOf(units))
	}
}

func TestInlineHTMLDoesNotMakeAParagraphAnHTMLBlock(t *testing.T) {
	units := Parse("t.md", "a paragraph with an <b>inline</b> tag in it\n")
	eq(t, kindsOf(units), []string{"paragraph@1"})
}

func TestHTMLBlocksCarryTheHeading(t *testing.T) {
	units := Parse("t.md", "# Section\n\n<div>\nx\n</div>\n")
	for _, u := range units {
		if u.Kind == "html" && u.Heading != "# Section" {
			t.Errorf("html block lost its heading: %q", u.Heading)
		}
	}
}

// --- table rows ------------------------------------------------------------

func TestEachTableRowIsItsOwnUnit(t *testing.T) {
	units := Parse("t.md", "| a | b |\n| --- | --- |\n| one | two |\n| three | four |\n")
	eq(t, kindsOf(units), []string{"table@1", "table@3", "table@4"})
}

// The separator carries no content, so it is not a unit.
func TestTheSeparatorRowIsNotAUnit(t *testing.T) {
	units := Parse("t.md", "| a | b |\n| --- | --- |\n| one | two |\n")
	for _, u := range units {
		if strings.Contains(u.Raw(), "---") {
			t.Errorf("the separator row became a unit: %q", u.Raw())
		}
	}
}

func TestATableRowKeepsItsHeadingAndItsOwnLine(t *testing.T) {
	units := Parse("t.md", "## Data\n\n| k | v |\n| --- | --- |\n| alpha | 1 |\n| beta | 2 |\n")
	var rows []Unit
	for _, u := range units {
		if u.Kind == "table" {
			rows = append(rows, u)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 table units (header + 2 rows), got %d", len(rows))
	}
	if rows[2].Line != 6 {
		t.Errorf("last row reports line %d, want 6", rows[2].Line)
	}
	for _, r := range rows {
		if r.Heading != "## Data" {
			t.Errorf("row lost its heading: %q", r.Heading)
		}
	}
}

// A hit in one row must not drag the whole table into the result, which is the
// noise this change exists to remove.
func TestAHitInOneRowReturnsOnlyThatRow(t *testing.T) {
	doc := "| k | v |\n| --- | --- |\n| alpha | 1 |\n| needle | 2 |\n| gamma | 3 |\n"
	ms, err := Search(Parse("t.md", doc), []string{"needle"}, Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(ms) != 1 {
		t.Fatalf("want 1 matching row, got %d", len(ms))
	}
	if strings.Contains(ms[0].U.Flat(), "gamma") {
		t.Errorf("the match dragged in another row: %q", ms[0].U.Flat())
	}
}
