package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/emezav/markgrep/search"
)

// Finding is something a check found. It is never a verdict about the document:
// it is a fact with its location.
type Finding struct {
	Line int
	What string
}

// Detector is a mechanical check WITH its positive control. The two travel
// together on purpose: a detector without a control may be dead and return
// zeros that measure nothing, and from the outside that reads exactly like a
// clean corpus.
type Detector struct {
	Name  string
	Desc  string
	Check func(lines []string) []Finding
	// Control returns a test input, how many findings it MUST produce, and a
	// fragment those findings MUST contain.
	//
	// The fragment is not decoration. A count of findings is invariant under any
	// bug that changes WHAT a finding says without changing HOW MANY there are --
	// measured on 2026-08-09 by making `nonASCII` iterate bytes instead of runes:
	// the output went from "1 character(s) outside ASCII" to "2", visibly wrong,
	// and the control still printed MEASURES because both are one finding on one
	// line. **A control whose expectation cannot vary with the defect certifies
	// it.** The fragment is what makes this one able to fail. Found by applying a
	// rule stele derived from our own 94.1%: a number without an expectation that
	// something could violate is decoration.
	Control func() (input []string, expected int, wants string)
	// Applicable reports whether THIS input can exercise Check at all, and says
	// why not when it cannot.
	//
	// Control proves the check discriminates; it runs against its own synthetic
	// input and therefore says nothing about what arrived. Without Applicable a
	// detector that could not possibly have fired still prints "0 findings", and
	// that zero is indistinguishable from a clean corpus -- the false absence
	// this tool exists to remove, occurring inside the subcommand written to
	// prevent it. Measured on an empty file, 2026-08-09, after stele asked.
	Applicable func(lines []string) (ok bool, why string)
}

func nonASCII(lines []string) []Finding {
	var out []Finding
	for i, l := range lines {
		var bad []string
		for _, r := range l {
			if r != '\t' && (r < 0x20 || r > 0x7E) {
				bad = append(bad, string(r))
			}
		}
		if len(bad) > 0 {
			out = append(out, Finding{i + 1, fmt.Sprintf("%d character(s) outside ASCII: %s",
				len(bad), strings.Join(bad, " "))})
		}
	}
	return out
}

// cells counts the separators of a table row.
//
// A pipe preceded by a backslash is ESCAPED: markdown reads it as content
// inside a cell, not as a boundary between two. Counting it cost two false
// findings against correctly written rows, both in delivered correspondence.
func cells(l string) int {
	n := 0
	for i := 0; i < len(l); i++ {
		if l[i] == '|' && (i == 0 || l[i-1] != '\\') {
			n++
		}
	}
	return n - 1
}

// tableRow is a content-carrying row, with which table of the document it
// belongs to.
type tableRow struct {
	Line  int // 1-based
	Cells int
	Table int // 0-based index of the table within the document
}

// tableRows walks a document and returns its table rows, grouped by table.
//
// One walker for the check AND for its applicability, because they answer the
// same question about the same shape and answering it twice is how the two
// drift apart. A table ends at the first line that is not one of its rows, and
// a line inside a fenced block is not a row at all -- these documents are full
// of code blocks whose content begins with a pipe.
func tableRows(lines []string) []tableRow {
	var out []tableRow
	table, inTable, inFence := -1, false, false
	for i, l := range lines {
		if search.IsFence(l) {
			inFence = !inFence
			inTable = false
			continue
		}
		if inFence {
			continue
		}
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "|") {
			inTable = false
			continue
		}
		if strings.Contains(t, "---") {
			continue // separator row: part of the table, carries no content
		}
		if !inTable {
			table++
			inTable = true
		}
		out = append(out, tableRow{i + 1, cells(t), table})
	}
	return out
}

// raggedColumns reports rows whose cell count differs from their OWN table's
// header.
//
// It used to compare against the first table row in the whole FILE, so a
// well-formed second table of a different width had every row reported. Measured
// on 2026-08-09 over 117 documents: 356 findings before, 2 after, and both
// survivors are real.
func raggedColumns(lines []string) []Finding {
	var out []Finding
	cur, ref, refLine := -1, 0, 0
	for _, r := range tableRows(lines) {
		if r.Table != cur {
			cur, ref, refLine = r.Table, r.Cells, r.Line
			continue
		}
		if r.Cells != ref {
			out = append(out, Finding{r.Line,
				fmt.Sprintf("%d columns; the header (line %d) has %d", r.Cells, refLine, ref)})
		}
	}
	return out
}

// hasContent reports whether there is any character for nonASCII to look at.
func hasContent(lines []string) (bool, string) {
	for _, l := range lines {
		if l != "" {
			return true, ""
		}
	}
	return false, "the input has no content"
}

// unquote strips one level of blockquote marker so the SAME table walker can be
// asked a second question -- "would there be rows if this were not quoted?" --
// without anyone writing a second definition of what a table row is.
//
// It is used only to explain a refusal. Nothing measured ever comes from it: a
// quote is one unit and stays one, and reporting ragged columns inside one would
// silently change what a finding means, because a quoted table in a letter is
// usually a table archived verbatim from somebody else.
func unquote(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if strings.HasPrefix(t, ">") {
			t = strings.TrimPrefix(t, ">")
			t = strings.TrimPrefix(t, " ")
			out[i] = t
			continue
		}
		out[i] = l
	}
	return out
}

// hasComparableTable reports whether SOME table in the input has a header AND
// at least one row to compare against it.
//
// It walks the same `tableRows` the check does, which is the point: counting
// rows across the whole file said yes for a document with two single-row
// tables, where the check can compare nothing. An applicability answer that
// does not use the check's own notion of a table is a guard for a different
// function.
func hasComparableTable(lines []string) (bool, string) {
	rows := tableRows(lines)
	if len(rows) == 0 {
		// A refusal has to say WHICH refusal it is. stele reported, from their
		// own archive, that "the input has no table" was false for every letter
		// whose claims table sits inside a blockquote -- their convention -- and
		// that a false "nothing here" reads exactly like "looked, and fine". It
		// had been silently blind over 18 of their files, and over 14 of the 54
		// letters in ours.
		//
		// The unit model does not change: a quote is a quote, and the check
		// still measures nothing there. What changes is that the two refusals
		// stop sounding alike. The question is asked by handing the SAME walker
		// a stripped copy, never by writing a second notion of a table row --
		// two of those in one binary is the shape this project has paid for
		// twice.
		if len(tableRows(unquote(lines))) > 0 {
			return false, "the only table rows here live inside a blockquote, which this detector does not read"
		}
		return false, "the input has no table"
	}
	perTable := map[int]int{}
	for _, r := range rows {
		perTable[r.Table]++
	}
	for _, n := range perTable {
		if n >= 2 {
			return true, ""
		}
	}
	return false, "no table in the input has a row to compare against its header"
}

func Detectors() []Detector {
	return []Detector{
		{
			Name:  "ascii",
			Desc:  "no character outside printable ASCII",
			Check: nonASCII,
			Control: func() ([]string, int, string) {
				// One finding, and it must say ONE character. The n-tilde is one
				// character and two bytes, which is written down in
				// bitacora/memory.md -- so this expectation was derivable from
				// something the project already had, not invented here.
				return []string{"a line with an n-tilde: año"}, 1, "1 character(s)"
			},
			Applicable: hasContent,
		},
		{
			Name:  "columns",
			Desc:  "every table row has its header's column count",
			Check: raggedColumns,
			// The control carries ONE INSTANCE OF EVERY DISTINCTION THE CHECK
			// MAKES, which is what a single ragged table could never do.
			//
			// stele derived the narrow form of this from our own failure: a
			// control of one instance cannot validate a detector that compares
			// against a reference, because with one table both references
			// coincide by construction. That is why five versions of a detector
			// with under one percent signal printed MEASURES every time -- a
			// control does not fall short noisily, it certifies.
			//
			// Generalised here: the input has two tables of different widths (so
			// taking the reference from the file goes BROKEN), an escaped pipe
			// that is content (so counting it goes BROKEN), and a fenced block
			// whose lines start with a pipe (so reading them as rows goes
			// BROKEN). Exactly one row is really ragged.
			Control: func() ([]string, int, string) {
				return []string{
					"| a | b |",
					"| --- | --- |",
					"| 1 | 2 |",
					"",
					"| c | d | e |",
					"| --- | --- | --- |",
					"| 1 | 2 | 3 |",
					`| x \| y | z | w |`,
					"| 4 | 5 |",
					"",
					"```text",
					"| a drawing | not | a table |",
					"| shorter |",
					"```",
				}, 1, "2 columns; the header (line 5) has 3"
			},
			Applicable: hasComparableTable,
		},
	}
}

// Verify runs the detectors and prints, FOR EACH ONE and in the same output,
// its positive control next to its result. A "clean" must never be readable
// without seeing, right beside it, that the detector was measuring something.
//
// Returns (findings, brokenDetectors, measured), where measured counts the
// detectors that actually ran against THIS input.
//
// The caller needs the third number to tell two zeros apart. Zero findings out
// of two detectors that measured is a clean corpus; zero findings out of zero
// detectors that measured is no information at all, and reporting both as
// success is the false absence this tool exists to remove.
func Verify(w io.Writer, path string, lines []string, lastOnly bool) (int, int, int) {
	return verifyWith(w, path, lines, lastOnly, Detectors())
}

// verifyWith is Verify over an explicit detector list, so the suite can hand it
// a deliberately broken detector and watch it be refused. A guard that cannot be
// proven red is decoration, which is the defect this file exists to prevent.
func verifyWith(w io.Writer, path string, lines []string, lastOnly bool, ds []Detector) (int, int, int) {
	// The focus narrows what is REPORTED, not what the detector READS.
	//
	// Trimming the input looked equivalent and is not: the column detector needs
	// the table header to have something to compare against, so with a single
	// line it returned zero ALWAYS -- a dead detector whose positive control
	// still passed, because the control tests the DETECTOR and not the SCOPE.
	focus := 0
	scope := fmt.Sprintf("%s (%d lines)", path, len(lines))
	if lastOnly && len(lines) > 0 {
		focus = len(lines)
		scope = fmt.Sprintf("%s (whole file, reporting only line %d)", path, focus)
	}
	filter := func(f []Finding) []Finding {
		if focus == 0 {
			return f
		}
		var out []Finding
		for _, x := range f {
			if x.Line == focus {
				out = append(out, x)
			}
		}
		return out
	}

	total, broken, skipped, measured := 0, 0, 0, 0
	fmt.Fprintf(w, "scope: %s\n", scope)

	for _, d := range ds {
		input, expected, wants := d.Control()
		found := d.Check(input)
		got := len(found)
		var said []string
		for _, f := range found {
			said = append(said, f.What)
		}
		joined := strings.Join(said, " | ")

		// The ways to be BROKEN, and the first two are about the detector's
		// ability to look rather than its result. A detector that cannot say
		// whether it could measure is in the same position as one that cannot
		// prove it measures, and a control run on an input the detector would
		// refuse proves nothing at all.
		state, why := "MEASURES", ""
		switch {
		case d.Applicable == nil:
			state, why = "BROKEN", "no applicability check"
		case got != expected:
			state, why = "BROKEN", "the control did not produce what it must"
		case wants != "" && !strings.Contains(joined, wants):
			state, why = "BROKEN", fmt.Sprintf("the finding does not say %q", wants)
		default:
			if ok, _ := d.Applicable(input); !ok {
				state, why = "BROKEN", "the control input is one this detector would refuse"
			}
		}
		if state == "BROKEN" {
			broken++
		}

		fmt.Fprintf(w, "\ndetector %-9s %s\n", d.Name, d.Desc)
		fmt.Fprintf(w, "  positive control: expected %d, got %d  -> %s", expected, got, state)
		if why != "" {
			fmt.Fprintf(w, " (%s)", why)
		}
		fmt.Fprintln(w)

		if state == "BROKEN" {
			fmt.Fprintf(w, "  result: NOT REPORTED. A detector that does not measure cannot say 'clean'.\n")
			continue
		}

		// Scope again, one level in from --last: the check is healthy and the
		// input still cannot exercise it. Reporting "0 findings" here would be a
		// zero about the corpus when it is a zero about the question.
		if ok, reason := d.Applicable(lines); !ok {
			skipped++
			fmt.Fprintf(w, "  result: NOT APPLICABLE -- %s\n", reason)
			continue
		}
		measured++
		f := filter(d.Check(lines))
		total += len(f)
		if len(f) == 0 {
			fmt.Fprintf(w, "  result: 0 findings\n")
			continue
		}
		for _, x := range f {
			fmt.Fprintf(w, "  result: line %d -> %s\n", x.Line, x.What)
		}
	}

	fmt.Fprintf(w, "\n--- grep equivalent (no positive control: a broken zero reads the same) ---\n")
	fmt.Fprintf(w, "  LC_ALL=C grep -n '[^ -~]' %s\n", path)
	fmt.Fprintf(w, "  awk -F'|' '{print NF-2}' %s\n", path)
	fmt.Fprintf(w, "\nfindings: %d   broken detectors: %d   not applicable: %d   measured: %d\n",
		total, broken, skipped, measured)

	// The tail carries the consequence, not just the count. A person reading
	// "0 findings" beside "measured: 0" should not have to work out what it
	// means for the exit code.
	if measured == 0 {
		fmt.Fprintf(w, "note: nothing could measure this input, so this run cannot say clean (exit 2).\n")
	} else if skipped > 0 && total == 0 {
		fmt.Fprintf(w, "note: %d detector(s) could not be exercised, so this zero covers less than the whole check.\n", skipped)
	}
	return total, broken, measured
}
