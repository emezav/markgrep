package main

import (
	"os"
	"path/filepath"
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
	Kind    string // paragraph | item | code | table | heading | quote
	Lines   []string
}

// Raw returns the unit exactly as written.
func (u Unit) Raw() string { return strings.Join(u.Lines, "\n") }

// Flat returns the unit ready to be searched: newlines collapsed to spaces,
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
func (u Unit) Flat() string {
	return Normalize(strings.Join(u.Lines, " "))
}

// Normalize is applied to the text always, and to the pattern only in literal
// mode. Applying it to a regular expression eats its operators.
func Normalize(s string) string {
	s = strings.NewReplacer("*", "", "`", "", "~", "").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func isHeading(l string) (string, bool) {
	t := strings.TrimLeft(l, " ")
	if strings.HasPrefix(t, "#") {
		return strings.TrimSpace(t), true
	}
	return "", false
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

func isTableRow(l string) bool { return strings.HasPrefix(strings.TrimLeft(l, " "), "|") }
func isQuote(l string) bool    { return strings.HasPrefix(strings.TrimLeft(l, " "), ">") }

// Parse splits a markdown document into units. The rules are deliberately
// simple: more fidelity to the spec would buy nothing for what this does.
func Parse(path string, content string) []Unit {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var out []Unit
	heading := ""

	i := 0
	for i < len(lines) {
		l := lines[i]

		if strings.HasPrefix(strings.TrimLeft(l, " "), "```") {
			start := i
			i++
			for i < len(lines) {
				fence := strings.HasPrefix(strings.TrimLeft(lines[i], " "), "```")
				i++
				if fence {
					break
				}
			}
			out = append(out, Unit{path, start + 1, heading, "code", lines[start:i]})
			continue
		}

		if strings.TrimSpace(l) == "" {
			i++
			continue
		}

		if h, ok := isHeading(l); ok {
			heading = h
			out = append(out, Unit{path, i + 1, heading, "heading", []string{l}})
			i++
			continue
		}

		kind := "paragraph"
		switch {
		case isListItem(l):
			kind = "item"
		case isTableRow(l):
			kind = "table"
		case isQuote(l):
			kind = "quote"
		}

		start := i
		i++
		for i < len(lines) {
			s := lines[i]
			if strings.TrimSpace(s) == "" {
				break
			}
			if _, ok := isHeading(s); ok {
				break
			}
			if strings.HasPrefix(strings.TrimLeft(s, " "), "```") {
				break
			}
			if kind == "item" && isListItem(s) {
				break
			}
			if kind != "table" && isTableRow(s) {
				break
			}
			if kind == "table" && !isTableRow(s) {
				break
			}
			i++
		}
		out = append(out, Unit{path, start + 1, heading, kind, lines[start:i]})
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
