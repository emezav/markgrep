package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// --- setext headings -------------------------------------------------------

func TestSetextHeadingsAreHeadings(t *testing.T) {
	units := Parse("t.md", "Title one\n=========\n\nbody a\n\nTitle two\n---------\n\nbody b\n")
	eq(t, kindsOf(units), []string{"heading@1", "paragraph@4", "heading@6", "paragraph@9"})
}

// The regression that motivated all of this: the same paragraph, the same
// query, must report the section it lives under whichever heading syntax the
// document uses. Before this, a setext document reported "(no section)" for
// everything, and that reads as "this file has no structure".
func TestSetextAndAtxAttributeTheSameHeading(t *testing.T) {
	atx := Parse("t.md", "# Section\n\nbody text\n")
	setext := Parse("t.md", "Section\n=======\n\nbody text\n")

	var got [2]string
	for i, units := range [][]Unit{atx, setext} {
		for _, u := range units {
			if u.Kind == "paragraph" {
				got[i] = u.Heading
			}
		}
	}
	if got[0] == "" || got[1] == "" {
		t.Fatalf("a paragraph came back with no section: atx=%q setext=%q", got[0], got[1])
	}
	if !strings.Contains(got[1], "Section") {
		t.Errorf("setext heading not attributed: got %q", got[1])
	}
}

// --- the ambiguity of "---" ------------------------------------------------

func TestDashesAfterABlankLineAreNotAHeading(t *testing.T) {
	units := Parse("t.md", "some text\n\n---\n\nmore text\n")
	for _, u := range units {
		if u.Kind == "heading" {
			t.Fatalf("a thematic break became a heading: %v", kindsOf(units))
		}
	}
}

// A run of dashes inside a table is its separator, never a setext underline and
// never a thematic break. It is dropped, and the rows around it survive as
// units of their own.
func TestATableSeparatorIsDroppedAndTheRowsSurvive(t *testing.T) {
	units := Parse("t.md", "| a | b |\n| --- | --- |\n| 1 | 2 |\n")
	eq(t, kindsOf(units), []string{"table@1", "table@3"})
	for _, u := range units {
		if u.Kind != "table" {
			t.Errorf("the separator turned a row into %q", u.Kind)
		}
	}
}

func TestADashListIsNotAnUnderline(t *testing.T) {
	units := Parse("t.md", "intro line\n\n- first item\n- second item\n")
	eq(t, kindsOf(units), []string{"paragraph@1", "item@3", "item@4"})
}

// --- front matter ----------------------------------------------------------

func TestFrontMatterIsItsOwnKind(t *testing.T) {
	units := Parse("t.md", "---\ntitle: A title\ntags: [x]\n---\n\nbody text\n")
	eq(t, kindsOf(units), []string{"frontmatter@1", "paragraph@6"})
}

func TestFrontMatterOnlyCountsAtTheTop(t *testing.T) {
	units := Parse("t.md", "body first\n\n---\ntitle: not front matter\n---\n")
	if units[0].Kind == "frontmatter" {
		t.Errorf("front matter recognised away from the top: %v", kindsOf(units))
	}
}

func TestUnterminatedFrontMatterIsNotFrontMatter(t *testing.T) {
	units := Parse("t.md", "---\ntitle: never closed\n\nbody\n")
	for _, u := range units {
		if u.Kind == "frontmatter" {
			t.Errorf("an unterminated block was read as front matter: %v", kindsOf(units))
		}
	}
}

// --- indented code ---------------------------------------------------------

func TestIndentedBlockIsCode(t *testing.T) {
	units := Parse("t.md", "intro\n\n    indented line one\n    indented line two\n\ntail\n")
	eq(t, kindsOf(units), []string{"paragraph@1", "code@3", "paragraph@6"})
}

// The trap named in the checkpoint: after a list item, the same indentation is
// the item's own continuation. Calling it code would hide prose that a reader
// sees as prose.
func TestIndentedLinesAfterAListItemAreNotCode(t *testing.T) {
	units := Parse("t.md", "- an item\n\n    continuation of the item\n")
	for _, u := range units {
		if u.Kind == "code" {
			t.Fatalf("a list continuation became code: %v", kindsOf(units))
		}
	}
}

func TestIndentedCodeDoesNotSwallowTheFollowingParagraph(t *testing.T) {
	units := Parse("t.md", "    code line\n\nprose after the block\n")
	eq(t, kindsOf(units), []string{"code@1", "paragraph@3"})
}

// --- links -----------------------------------------------------------------

func TestLinkModes(t *testing.T) {
	const in = "see [the whole idea](https://example.com/x) continues"
	cases := []struct{ mode, want string }{
		{LinksText, "see the whole idea continues"},
		{LinksURL, "see https://example.com/x continues"},
		{LinksBoth, "see the whole idea https://example.com/x continues"},
	}
	for _, c := range cases {
		if got := NormalizeWith(in, c.mode); got != c.want {
			t.Errorf("NormalizeWith(%s) = %q, want %q", c.mode, got, c.want)
		}
	}
}

// The second construct, after emphasis, that splits a phrase nobody wrote as
// split.
func TestPhraseAcrossALinkIsReachable(t *testing.T) {
	units := Parse("t.md", "text with [a descriptive label](https://example.com/very/long)\nand the phrase continues.\n")
	if !strings.Contains(units[0].Flat(), "a descriptive label and the phrase continues") {
		t.Errorf("the URL still splits the phrase: %q", units[0].Flat())
	}
}

func TestImagesAndReferenceLinksResolveToTheirText(t *testing.T) {
	if got, want := Normalize("![alt text](img.png) after"), "alt text after"; got != want {
		t.Errorf("image: got %q, want %q", got, want)
	}
	if got, want := Normalize("a [ref label][1] here"), "a ref label here"; got != want {
		t.Errorf("reference link: got %q, want %q", got, want)
	}
	if got, want := Normalize("bare <https://example.com/a> link"), "bare https://example.com/a link"; got != want {
		t.Errorf("autolink: got %q, want %q", got, want)
	}
}

func TestLinkTitleIsDropped(t *testing.T) {
	if got, want := Normalize(`[label](http://x "the title") tail`), "label tail"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- kind selection --------------------------------------------------------

func TestKindAllowedAcceptsAList(t *testing.T) {
	cases := []struct {
		kind, spec string
		want       bool
	}{
		{"item", "", true},
		{"item", "any", true},
		{"item", "item", true},
		{"item", "paragraph,item", true},
		{"item", "paragraph, item", true},
		{"item", "paragraph,code", false},
		{"frontmatter", "frontmatter", true},
	}
	for _, c := range cases {
		if got := kindAllowed(c.kind, c.spec); got != c.want {
			t.Errorf("kindAllowed(%q, %q) = %v, want %v", c.kind, c.spec, got, c.want)
		}
	}
}

func TestCheckKindsRejectsATypo(t *testing.T) {
	if err := checkKinds("paragraph,itemm"); err == nil {
		t.Error("an unknown kind must be refused, not silently match nothing")
	}
	if err := checkKinds("paragraph,item,any"); err != nil {
		t.Errorf("a valid list was refused: %v", err)
	}
	for _, k := range Kinds {
		if err := checkKinds(k); err != nil {
			t.Errorf("documented kind %q refused: %v", k, err)
		}
	}
}

func TestCheckLinksRejectsATypo(t *testing.T) {
	if err := checkLinks("texts"); err == nil {
		t.Error("an unknown --links mode must be refused")
	}
	for _, m := range []string{"", LinksText, LinksURL, LinksBoth} {
		if err := checkLinks(m); err != nil {
			t.Errorf("valid mode %q refused: %v", m, err)
		}
	}
}

// The help text and the validator must not drift: a validated flag whose help
// lists the wrong names refuses something it never advertised. This caught the
// real cost of validating --kind -- the list had two homes and adding `html`
// updated only one.
func TestUsageAdvertisesEveryValidKind(t *testing.T) {
	for _, k := range Kinds {
		if !strings.Contains(usage, k) {
			t.Errorf("usage text does not mention the valid kind %q", k)
		}
	}
	if !strings.Contains(usage, "| any") {
		t.Error("usage text does not mention `any`")
	}
	if strings.Contains(usage, "{{kinds}}") {
		t.Error("the usage template was not resolved")
	}
}

// The README is the product's front page, so the kind list stays visible there
// rather than being replaced by a pointer. That leaves the list with two homes
// -- Kinds and the README -- which is the defect stele found in bc36115, where
// main.go generated its help and README.md still enumerated by hand.
//
// This does not eliminate the second home; it makes drift impossible to ship.
// Eliminating would mean generating the README at build time, and this project
// has no build step beyond `go build`.
//
// The comparison runs BOTH ways. A guard that only walks Kinds sees what is
// missing and never what is surplus: drop a kind from Kinds, leave it in the
// README, and a one-way test stays green while the README advertises a name the
// tool now refuses. stele asked the question that found that hole.
func TestReadmeKindListMatchesKindsExactly(t *testing.T) {
	names := readmeKinds(t)

	want := map[string]bool{"any": true}
	for _, k := range Kinds {
		want[k] = true
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}

	for k := range want {
		if !got[k] {
			t.Errorf("README.md does not list %q", k)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("README.md lists %q, which is not a valid kind -- the tool would refuse it", n)
		}
	}
}

// readmeKinds returns the names enumerated in the README's --kind row.
//
// It fails rather than returning nothing when it cannot find the list. A
// locator that silently finds no list turns the guard above into decoration,
// which is the same class of defect the guard exists to catch.
func readmeKinds(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("cannot read README.md, so this test proves nothing: %v", err)
	}
	const marker = "unit kinds:"
	for _, line := range strings.Split(string(b), "\n") {
		i := strings.Index(line, marker)
		if i < 0 {
			continue
		}
		names := regexp.MustCompile("`([a-z]+)`").FindAllStringSubmatch(line[i+len(marker):], -1)
		if len(names) == 0 {
			t.Fatalf("found the %q row but no backticked names in it: %s", marker, line)
		}
		out := make([]string, len(names))
		for j, m := range names {
			out[j] = m[1]
		}
		return out
	}
	t.Fatalf("README.md has no line containing %q; the guard cannot locate the kind list", marker)
	return nil
}

// Inside a fence a line break is content, not a wrap. A phrase built from the
// end of one command and the start of the next exists in no command anyone
// could copy, and returning it is the false positive this tool exists to
// remove. Measured before deciding: 214 such adjacencies in 124 documents, and
// every one of them joined two unrelated lines.
func TestFencedLinesDoNotJoinAcrossCommands(t *testing.T) {
	doc := "```sh\ngit clone x .stele && rm -rf .stele/.git\n# alternativa con npx\n```\n"
	u := Parse("t.md", doc)
	if len(u) != 1 || u[0].Kind != "code" {
		t.Fatalf("expected one code unit, got %d", len(u))
	}
	flat := u[0].Flat()
	if strings.Contains(flat, "rm -rf .stele/.git # alternativa") {
		t.Errorf("a phrase across two unrelated command lines still matches:\n%q", flat)
	}
	// The negative control, in the same test: each line is still findable whole.
	for _, want := range []string{"rm -rf .stele/.git", "# alternativa con npx"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the line itself stopped being findable: %q not in %q", want, flat)
		}
	}
}

// The one case that argued for the old behaviour: a backslash continues the
// line, so the two are one command and a phrase across them is real. The
// backslash is dropped so the logical command reads as written.
func TestFencedContinuationStillJoins(t *testing.T) {
	// Built by concatenation so the continuation backslash is unambiguous: an
	// escaped one inside an interpreted string is exactly the character a shell
	// here-document eats, and it did while this test was being written.
	doc := "```sh\n" + `grep -rn foo \` + "\n  --include=bar baz\n```\n"
	u := Parse("t.md", doc)
	flat := u[0].Flat()
	if !strings.Contains(flat, "grep -rn foo --include=bar baz") {
		t.Errorf("a continuation must read as one command:\n%q", flat)
	}
	if strings.Contains(flat, "\\") {
		t.Errorf("the continuation backslash should not survive into the searchable form:\n%q", flat)
	}
}

// The founding case must be untouched: outside a fence, a hard wrap is still
// crossed. This is the whole reason the tool exists, and the change above is
// only safe because it is scoped to one kind.
func TestProseStillCrossesItsWraps(t *testing.T) {
	doc := "a phrase that the author\nwrapped in the middle\n"
	u := Parse("t.md", doc)
	if got := u[0].Flat(); !strings.Contains(got, "the author wrapped in the middle") {
		t.Errorf("prose stopped crossing a wrap, which is the founding case:\n%q", got)
	}
}
