package main

import (
	"fmt"
	"io"
	"strings"
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
	// Control returns a test input and how many findings it MUST produce.
	Control func() (input []string, expected int)
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

func raggedColumns(lines []string) []Finding {
	cells := func(l string) int { return strings.Count(l, "|") - 1 }
	ref, refLine := -1, 0
	var out []Finding
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		if strings.Contains(t, "---") {
			continue // separator row
		}
		n := cells(t)
		if ref == -1 {
			ref, refLine = n, i+1
			continue
		}
		if n != ref {
			out = append(out, Finding{i + 1,
				fmt.Sprintf("%d columns; the header (line %d) has %d", n, refLine, ref)})
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

// hasComparableTable reports whether raggedColumns has a header AND at least
// one row to compare against it. With a single row there is nothing to differ
// from, so the check returns zero for a reason that has nothing to do with the
// document being well formed.
func hasComparableTable(lines []string) (bool, string) {
	rows := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "|") && !strings.Contains(t, "---") {
			rows++
		}
	}
	switch rows {
	case 0:
		return false, "the input has no table"
	case 1:
		return false, "the input has a table header and no row to compare against it"
	}
	return true, ""
}

func Detectors() []Detector {
	return []Detector{
		{
			Name:  "ascii",
			Desc:  "no character outside printable ASCII",
			Check: nonASCII,
			Control: func() ([]string, int) {
				// If this does not return 1, the detector is not looking at what
				// it claims to look at.
				return []string{"a line with an n-tilde: año"}, 1
			},
			Applicable: hasContent,
		},
		{
			Name:  "columns",
			Desc:  "every table row has its header's column count",
			Check: raggedColumns,
			Control: func() ([]string, int) {
				return []string{"| a | b | c |", "| --- | --- | --- |", "| 1 | 2 | 3 | 4 |"}, 1
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
		input, expected := d.Control()
		got := len(d.Check(input))

		// Three ways to be BROKEN, and the first two are about the detector's
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
