package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// kindsOf reduces a parse to "kind@line" strings, which is what almost every
// boundary assertion in here actually cares about.
func kindsOf(units []Unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Kind + "@" + strconv.Itoa(u.Line)
	}
	return out
}

// write creates a file and the directories above it, so a test can describe a
// corpus in one line.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d units %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("unit %d: got %q, want %q\n  full: %v", i, got[i], want[i], got)
		}
	}
}

func TestParseKindsAndBoundaries(t *testing.T) {
	doc := strings.Join([]string{
		"# Heading one",   // 1  heading
		"",                // 2
		"A paragraph",     // 3  paragraph
		"that wraps.",     // 4
		"",                // 5
		"- item one",      // 6  item
		"  continued",     // 7
		"- item two",      // 8  item (a sibling item breaks the previous one)
		"",                // 9
		"> a quote",       // 10 quote
		"> more quote",    // 11
		"",                // 12
		"| a | b |",       // 13 table
		"| --- | --- |",   // 14
		"| 1 | 2 |",       // 15
		"",                // 16
		"```go",           // 17 code, fences included
		"x := 1",          // 18
		"```",             // 19
		"",                // 20
		"Tail paragraph.", // 21
	}, "\n")

	units := Parse("t.md", doc)
	// Line 14 is the separator and never becomes a unit; lines 13 and 15 are one
	// unit each, which is the contract change of 2026-08-08.
	eq(t, kindsOf(units), []string{
		"heading@1", "paragraph@3", "item@6", "item@8",
		"quote@10", "table@13", "table@15", "code@17", "paragraph@21",
	})
}

func TestHeadingIsAttributedToFollowingUnits(t *testing.T) {
	doc := "# First\n\npara a\n\n## Second\n\npara b\n"
	units := Parse("t.md", doc)

	var headings []string
	for _, u := range units {
		if u.Kind == "paragraph" {
			headings = append(headings, u.Heading)
		}
	}
	eq(t, headings, []string{"# First", "## Second"})
}

func TestFencedBlockKeepsItsFences(t *testing.T) {
	units := Parse("t.md", "```\nline\n```\n")
	if len(units) != 1 {
		t.Fatalf("want 1 unit, got %d: %v", len(units), kindsOf(units))
	}
	if got, want := len(units[0].Lines), 3; got != want {
		t.Fatalf("fenced unit should carry %d lines including both fences, got %d", want, got)
	}
}

func TestNormalizeStripsMarkupButKeepsUnderscores(t *testing.T) {
	cases := []struct{ in, want string }{
		{"**Rule of thumb:** do not", "Rule of thumb: do not"},
		{"a `code` span", "a code span"},
		{"~~struck~~ out", "struck out"},
		{"audit_log and max_retries", "audit_log and max_retries"},
		{"  collapse   runs \t of space ", "collapse runs of space"},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFlatJoinsLinesAndRawPreservesThem(t *testing.T) {
	u := Unit{Lines: []string{"first line", "second **line**"}}
	if got, want := u.Flat(), "first line second line"; got != want {
		t.Errorf("Flat() = %q, want %q", got, want)
	}
	if got, want := u.Raw(), "first line\nsecond **line**"; got != want {
		t.Errorf("Raw() = %q, want %q", got, want)
	}
}

// The founding case: a phrase split by a hard wrap must be reachable.
func TestPhraseAcrossHardWrapIsReachable(t *testing.T) {
	units := Parse("t.md", "Rule of thumb: do not read a\nwhole file just in case.\n")
	if len(units) != 1 {
		t.Fatalf("want 1 paragraph, got %v", kindsOf(units))
	}
	if !strings.Contains(units[0].Flat(), "do not read a whole file") {
		t.Errorf("flattened unit lost the phrase across the wrap: %q", units[0].Flat())
	}
}

func TestOrderedListItemsAreItems(t *testing.T) {
	units := Parse("t.md", "1. first\n2) second\n")
	eq(t, kindsOf(units), []string{"item@1", "item@2"})
}

func TestCollectReadsASingleFileWhateverItsExtension(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/notes.txt", "# Heading\n\nbody text here\n")

	units, files, err := Collect(dir + "/notes.txt")
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %v", files)
	}
	if len(units) != 2 {
		t.Fatalf("want 2 units, got %v", kindsOf(units))
	}
}

func TestCollectWalkingADirectoryKeepsOnlyMarkdown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/a.md", "alpha\n")
	write(t, dir+"/b.markdown", "beta\n")
	write(t, dir+"/c.txt", "gamma\n")

	_, files, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 markdown files, got %v", files)
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".txt") {
			t.Errorf("directory walk picked up %s", f)
		}
	}
}

func TestCollectEmitsForwardSlashPaths(t *testing.T) {
	dir := t.TempDir()
	write(t, dir+"/sub/deep.md", "text\n")

	_, files, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %v", files)
	}
	if strings.Contains(files[0], "\\") {
		t.Errorf("path is not forward-slashed: %q", files[0])
	}
}
