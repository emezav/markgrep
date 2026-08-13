package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Unit is a semantic block of a markdown file: the grain we search in.
//
// It exists because grep works on LINES, and prose is hard-wrapped. Any phrase
// longer than three or four words may be split by a newline, so a correct query
// finds nothing and the failure reads exactly like an absence.
type Unit struct {
	Path    string
	Line    int    // 1-based, first line of the unit
	Heading string // nearest section heading above it
	Parent  string // for a nested list item: the item it hangs from
	Kind    string // paragraph | item | code | table | heading | quote | frontmatter | html
	Lines   []string
}

// Kinds are every value Kind can take. It exists so that --kind can reject a
// name it does not know instead of matching nothing: an unknown kind that
// silently returns zero is the false absence this tool exists to remove.
var Kinds = []string{"paragraph", "item", "code", "table", "heading", "quote", "frontmatter", "html"}

// Link modes decide what the text of a link contributes to the searchable form.
const (
	LinksText = "text" // [text](url) -> text        (default)
	LinksURL  = "url"  // [text](url) -> url
	LinksBoth = "both" // [text](url) -> text url
)

// Raw returns the unit exactly as written.
func (u Unit) Raw() string { return strings.Join(u.Lines, "\n") }

// Flat returns the unit ready to be searched, resolving links to their text.
func (u Unit) Flat() string { return u.FlatWith(LinksText) }

// FlatWith returns the unit ready to be searched: newlines collapsed to spaces,
// runs of whitespace reduced to one, and inline markup removed.
//
// Collapsing newlines kills hard wrapping. Removing markup kills a subtler
// failure: emphasis does not wrap words, it wraps ARBITRARY spans, punctuation
// included. A document may contain `**Rule of thumb:**` with the colon INSIDE
// the asterisks, so whoever writes the query cannot know where the author put
// them. Hard wrapping is at least predictable; this is not.
//
// Underscores are left alone on purpose: in technical prose they are part of
// identifiers (audit_log, max_retries) and stripping them would break the
// queries that do work.
func (u Unit) FlatWith(links string) string {
	if u.Kind == "code" {
		return flatCode(u.Lines, links)
	}
	return NormalizeWith(strings.Join(u.Lines, " "), links)
}

// flatCode flattens a code block WITHOUT joining its lines into one run.
//
// The reason the rest of this file collapses newlines does not hold here.
// Outside a fence a line break is where the author's editor happened to wrap,
// and a phrase crossing it is a phrase somebody wrote; inside a fence the break
// is content, so a phrase crossing it exists in no command anyone could copy.
// Returning it is a false positive of exactly the family this tool exists to
// remove -- measured on 2026-08-09 over 124 documents: flattening created 214
// adjacencies inside fences and every one of them joined two unrelated lines.
//
// The exception is the one case that argued for the old behaviour: a line
// ending in a backslash CONTINUES into the next, so the two are one command and
// a phrase across them is real. Those are joined with a space and the backslash
// is dropped, which is what makes the logical command searchable as written.
//
// Note that `strings.Fields` inside NormalizeWith collapses newlines too, so
// normalizing has to happen per segment rather than over the joined text. That
// is why this cannot be a one-line change to the separator.
func flatCode(lines []string, links string) string {
	var out []string
	var cur strings.Builder
	for _, l := range lines {
		t := strings.TrimRight(l, " \t")
		cont := strings.HasSuffix(t, `\`)
		if cont {
			t = strings.TrimSuffix(t, `\`)
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(NormalizeWith(t, links))
		if !cont {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return strings.Join(out, "\n")
}

var (
	// [text](url) and ![alt](url), with an optional title after the URL.
	reInlineLink = regexp.MustCompile(`!?\[([^\]]*)\]\(\s*<?([^)>\s]*)>?[^)]*\)`)
	// [text][ref] and [text][], which carry no URL of their own.
	reRefLink = regexp.MustCompile(`!?\[([^\]]*)\]\[[^\]]*\]`)
	// <https://example.com>, where the URL is also the visible text.
	reAutolink = regexp.MustCompile(`<((?:https?|ftp|mailto):[^>\s]+)>`)
	// A line break written as a tag separates words and must leave a space.
	reBreakTag = regexp.MustCompile(`(?i)<(?:br|hr)\s*/?>`)
	// Any other well-formed tag. It must start with a letter, which is what
	// keeps prose like "a < b" and "5 <= 6" out of its reach.
	reHTMLTag = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9-]*(?:\s[^<>]*)?/?>`)
)

// Normalize is applied to the text always, and to the pattern only in literal
// mode. Applying it to a regular expression eats its operators.
func Normalize(s string) string { return NormalizeWith(s, LinksText) }

// NormalizeWith is Normalize with the link mode made explicit.
//
// A link is the second construct, after inline emphasis, that splits a phrase
// nobody wrote as split: `[the whole idea](https://example.com/x) continues`
// puts a URL between two words that read as consecutive. Resolving the link to
// its visible text lets the phrase flow, which is the same reason newlines are
// collapsed. The URL is still reachable with --links url or --links both.
func NormalizeWith(s, links string) string {
	switch links {
	case LinksURL:
		s = reInlineLink.ReplaceAllString(s, "$2")
		s = reRefLink.ReplaceAllString(s, "")
	case LinksBoth:
		s = reInlineLink.ReplaceAllString(s, "$1 $2")
		s = reRefLink.ReplaceAllString(s, "$1")
	default: // LinksText
		s = reInlineLink.ReplaceAllString(s, "$1")
		s = reRefLink.ReplaceAllString(s, "$1")
	}
	// Autolinks first: <https://x> starts with a letter after the angle bracket
	// and would otherwise be mistaken for a tag and dropped.
	s = reAutolink.ReplaceAllString(s, "$1")
	// A tag is the third construct, after emphasis and links, that splits a
	// phrase nobody wrote as split: "an <b>important</b> idea". Removing it
	// leaves the words adjacent, which is what a reader sees. A break tag is
	// the exception -- it separates, so it leaves a space behind.
	s = reBreakTag.ReplaceAllString(s, " ")
	s = reHTMLTag.ReplaceAllString(s, "")
	s = inlineStripper.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

// InlineMarks are the inline emphasis characters removed from the searchable
// text. The underscore is deliberately NOT among them: in technical prose it is
// part of identifiers like audit_log, so stripping it would break the queries
// that do work -- which means `_emph_` survives where `*emph*` does not, and
// that asymmetry is real and worth knowing.
//
// This set has ONE home because it now has two readers. `search.go` consults it
// to explain an empty result whose pattern carries one of these, and a second
// copy there would drift the day a fourth mark is added -- the shape this
// project already paid for once, when the --kind help listed seven names and the
// validator accepted eight.
const InlineMarks = "*`~"

var inlineStripper = func() *strings.Replacer {
	pairs := make([]string, 0, 2*len(InlineMarks))
	for _, r := range InlineMarks {
		pairs = append(pairs, string(r), "")
	}
	return strings.NewReplacer(pairs...)
}()

func isHeading(l string) (string, bool) {
	t := strings.TrimLeft(l, " ")
	if strings.HasPrefix(t, "#") {
		return strings.TrimSpace(t), true
	}
	return "", false
}

// isSetextUnderline reports the level a line underlines: 1 for a run of '=', 2
// for a run of '-', 0 for anything else.
//
// The caller only asks about a line that already has paragraph text above it,
// and that is what makes the answer safe. On its own a run of dashes is a
// thematic break; under text it is a heading. A table separator starts with
// '|' and a list item needs a space after its marker, so neither reaches here
// looking like an underline.
func isSetextUnderline(l string) int {
	if len(l)-len(strings.TrimLeft(l, " ")) > 3 {
		return 0 // four spaces in means indented code, not an underline
	}
	t := strings.TrimRight(strings.TrimLeft(l, " "), " \t")
	if t == "" {
		return 0
	}
	if strings.Trim(t, "=") == "" {
		return 1
	}
	if strings.Trim(t, "-") == "" {
		return 2
	}
	return 0
}

func isListItem(l string) bool {
	t := strings.TrimLeft(l, " \t")
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' '
}

// indentOf reports how deep a line sits, counting a tab as four columns.
//
// It exists for one job -- telling a nested list item from its parent -- and it
// is deliberately not CommonMark's rule, which measures against the parent's
// content column. That fidelity would buy nothing here: the question is only
// which item a nested one hangs from.
func indentOf(l string) int {
	n := 0
	for _, r := range l {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4
		default:
			return n
		}
	}
	return n
}

func isTableRow(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), "|") }

// isTableSeparator reports the `| --- | --- |` row under a table header. It
// carries no content, so it never becomes a unit of its own.
func isTableSeparator(l string) bool {
	t := strings.TrimSpace(l)
	if !strings.HasPrefix(t, "|") {
		return false
	}
	return strings.Trim(t, "|-: \t") == "" && strings.Contains(t, "-")
}

// isHTMLBlockStart reports a line that opens a block-level HTML element or an
// HTML comment. A tag in the middle of a sentence is inline markup, not a
// block, so this is only ever asked at the start of a block.
func isHTMLBlockStart(l string) bool {
	t := strings.TrimLeft(l, " ")
	if strings.HasPrefix(t, "<!--") {
		return true
	}
	return reHTMLTag.MatchString(t) && strings.HasPrefix(t, "<")
}
func isQuote(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), ">") }
func isFence(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), "```") }

// isIndentedCode reports the four-space (or tab) form of a code block. It is
// only ever asked at the start of a block, because an indented line in the
// middle of one is a continuation, not code.
func isIndentedCode(l string) bool {
	return strings.HasPrefix(l, "    ") || strings.HasPrefix(l, "\t")
}

// frontMatter returns the length in lines of a YAML front matter block, or 0.
//
// It only counts at the very top of the file, and that restriction is the whole
// safety of it: three dashes anywhere else are a thematic break or a setext
// underline, and reading them as metadata would swallow prose.
func frontMatter(lines []string) int {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0
	}
	for j := 1; j < len(lines); j++ {
		switch strings.TrimSpace(lines[j]) {
		case "---", "...":
			return j + 1
		}
	}
	return 0 // unterminated: not front matter, let it parse as whatever it is
}

// Parse splits a markdown document into units. The rules are deliberately
// simple: more fidelity to the spec would buy nothing for what this does.
func Parse(path string, content string) []Unit {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var out []Unit
	heading := ""

	i := 0
	if n := frontMatter(lines); n > 0 {
		out = append(out, Unit{Path: path, Line: 1, Kind: "frontmatter", Lines: lines[0:n]})
		i = n
	}

	lastKind := ""
	// One entry per open nesting level, so a nested item can name the item it
	// hangs from. A unit already carries its nearest heading; nesting is the
	// same problem one level down, and the answer is the same shape.
	var open []struct {
		indent int
		text   string
	}
	for i < len(lines) {
		l := lines[i]

		if isFence(l) {
			start := i
			i++
			for i < len(lines) {
				fence := isFence(lines[i])
				i++
				if fence {
					break
				}
			}
			out = append(out, Unit{Path: path, Line: start + 1, Heading: heading, Kind: "code", Lines: lines[start:i]})
			lastKind = "code"
			continue
		}

		if strings.TrimSpace(l) == "" {
			i++
			continue
		}

		if h, ok := isHeading(l); ok {
			heading = h
			out = append(out, Unit{Path: path, Line: i + 1, Heading: heading, Kind: "heading", Lines: []string{l}})
			i++
			lastKind = "heading"
			continue
		}

		// An HTML block, and the comment form needs the same state a fence needs.
		// An unterminated comment is NOT a block: it falls through and parses as
		// prose, which is the same refusal frontMatter makes, and for the same
		// reason -- the alternative swallows the rest of the document.
		if isHTMLBlockStart(l) {
			start := i
			if strings.HasPrefix(strings.TrimLeft(l, " "), "<!--") {
				j, closed := i, false
				for j < len(lines) {
					done := strings.Contains(lines[j], "-->")
					j++
					if done {
						closed = true
						break
					}
				}
				if closed {
					out = append(out, Unit{Path: path, Line: start + 1, Heading: heading, Kind: "html", Lines: lines[start:j]})
					i = j
					lastKind = "html"
					continue
				}
			} else {
				j := i
				for j < len(lines) && strings.TrimSpace(lines[j]) != "" {
					j++
				}
				out = append(out, Unit{Path: path, Line: start + 1, Heading: heading, Kind: "html", Lines: lines[start:j]})
				i = j
				lastKind = "html"
				continue
			}
		}

		// One unit per table ROW, not one per table. A hit in a forty-row table
		// used to return all forty. The separator carries no content, so it is
		// skipped rather than made into an empty unit.
		if isTableRow(l) {
			if !isTableSeparator(l) {
				out = append(out, Unit{Path: path, Line: i + 1, Heading: heading, Kind: "table", Lines: []string{l}})
				lastKind = "table"
			}
			i++
			continue
		}

		// An indented block is code only when it opens a block of its own. After
		// a list item the same indentation is the item's continuation, and
		// calling it code would hide prose that a reader sees as prose.
		if lastKind != "item" && isIndentedCode(l) {
			start := i
			last := i
			for i < len(lines) && (strings.TrimSpace(lines[i]) == "" || isIndentedCode(lines[i])) {
				if strings.TrimSpace(lines[i]) != "" {
					last = i
				}
				i++
			}
			i = last + 1
			out = append(out, Unit{Path: path, Line: start + 1, Heading: heading, Kind: "code", Lines: lines[start:i]})
			lastKind = "code"
			continue
		}

		kind := "paragraph"
		switch {
		case isListItem(l):
			kind = "item"
		case isQuote(l):
			kind = "quote"
		}

		// A nested item hangs from the nearest less-indented one still open.
		// Anything that is not an item closes the list: a heading, a paragraph
		// or a table ends the nesting, and a blank line never reaches here.
		parent := ""
		if kind == "item" {
			ind := indentOf(l)
			for len(open) > 0 && open[len(open)-1].indent >= ind {
				open = open[:len(open)-1]
			}
			if len(open) > 0 {
				parent = open[len(open)-1].text
			}
		} else {
			open = open[:0]
		}

		start := i
		i++
		setext := 0
		for i < len(lines) {
			s := lines[i]
			if strings.TrimSpace(s) == "" {
				break
			}
			if _, ok := isHeading(s); ok {
				break
			}
			if isFence(s) {
				break
			}
			if kind == "item" && isListItem(s) {
				break
			}
			if isTableRow(s) {
				break
			}
			if kind == "paragraph" {
				if lvl := isSetextUnderline(s); lvl > 0 {
					setext = lvl
					i++ // the underline belongs to the heading
					break
				}
			}
			i++
		}

		if setext > 0 {
			heading = strings.TrimSpace(strings.Join(lines[start:i-1], " "))
			out = append(out, Unit{Path: path, Line: start + 1, Heading: heading, Kind: "heading", Lines: lines[start:i]})
			lastKind = "heading"
			continue
		}
		u := Unit{Path: path, Line: start + 1, Heading: heading, Parent: parent, Kind: kind, Lines: lines[start:i]}
		out = append(out, u)
		if kind == "item" {
			open = append(open, struct {
				indent int
				text   string
			}{indentOf(l), u.Flat()})
		}
		lastKind = kind
	}
	return out
}

// Collect reads the markdown files under a path (file or directory).
func Collect(root string) ([]Unit, []string, error) {
	var units []Unit
	var files []string

	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	add := func(p string) error {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(p))
		units = append(units, Parse(filepath.ToSlash(p), string(b))...)
		return nil
	}
	if !info.IsDir() {
		return units, files, add(root)
	}
	err = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if fi.Name() == ".git" || fi.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(fi.Name())
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") {
			return add(p)
		}
		return nil
	})
	return units, files, err
}
