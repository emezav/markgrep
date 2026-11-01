package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/emezav/markgrep/search"
)

// Output is how the CLI prints a result. It stayed out of search.Options when
// the library was split, because -c, -l and --raw change the printing and
// never which units come back.
type Output struct {
	Raw       bool // --raw: the unit with its line breaks
	Count     bool // -c: count only
	FilesOnly bool // -l: paths only
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
func PrintMatches(w io.Writer, r search.Result, patterns []string, root string, out Output, o search.Options) {
	ms := r.Matches
	if out.Count {
		fmt.Fprintf(w, "%d\n", len(ms))
		return
	}
	if out.FilesOnly {
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
		// The one zero this tool could not explain, reported by stele in their
		// letter 113 after it cost them a real count: --raw shows the source
		// with its asterisks, matching runs against the flattened text without
		// them, and nothing said so. The block marker survives, which is what
		// makes it convincing -- `^-` finds every item and `^- \*` finds none,
		// so the corpus and the pattern both look right.
		//
		// Only on the empty path, and only in regex mode: under -F the pattern
		// is normalized too, so that search already works and this advice would
		// be wrong. A regex whose asterisk is a legitimate quantifier trips it;
		// that costs one line on a result that was empty anyway.
		if !o.Literal {
			for _, p := range patterns {
				if strings.ContainsAny(p, search.InlineMarks) {
					fmt.Fprintf(w, "note: the pattern carries one of %q, which are removed from the text\n",
						search.InlineMarks)
					fmt.Fprintf(w, "      before matching. The block marker is kept. Try -F, which\n")
					fmt.Fprintf(w, "      normalizes the pattern the same way.\n")
					break
				}
			}
		}
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
		if !out.Raw {
			text = clip(m.U.FlatWith(o.Links), 600)
		}
		for _, l := range strings.Split(text, "\n") {
			fmt.Fprintf(w, "  | %s\n", l)
		}
	}

	// The invariant this block serves, and why its flags look the way they do,
	// lives on search.GrepEquivalent.
	//
	// -c and -l return before reaching here, deliberately: they are
	// machine-readable output and a caller may be piping them. DECIDED
	// 2026-08-10, when an audit found the invariant had been false for those two
	// paths since the first commit and nothing had ever declared it. Pinned by
	// TestPrintMatchesKeepsMachineReadableOutputClean.
	fmt.Fprintf(w, "\n--- grep equivalent (worse: line-based, misses anything the wrap split) ---\n")
	for _, g := range search.GrepEquivalent(patterns, root, o) {
		fmt.Fprintf(w, "  %s\n", g)
	}
	fmt.Fprintf(w, "\nmatching units: %d\n", len(ms))
}
