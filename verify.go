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
		},
		{
			Name:  "columns",
			Desc:  "every table row has its header's column count",
			Check: raggedColumns,
			Control: func() ([]string, int) {
				return []string{"| a | b | c |", "| --- | --- | --- |", "| 1 | 2 | 3 | 4 |"}, 1
			},
		},
	}
}

// Verify runs the detectors and prints, FOR EACH ONE and in the same output,
// its positive control next to its result. A "clean" must never be readable
// without seeing, right beside it, that the detector was measuring something.
//
// Returns (findings, brokenDetectors).
func Verify(w io.Writer, path string, lines []string, lastOnly bool) (int, int) {
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

	total, broken := 0, 0
	fmt.Fprintf(w, "scope: %s\n", scope)

	for _, d := range Detectors() {
		input, expected := d.Control()
		got := len(d.Check(input))
		state := "MEASURES"
		if got != expected {
			state = "BROKEN"
			broken++
		}
		fmt.Fprintf(w, "\ndetector %-9s %s\n", d.Name, d.Desc)
		fmt.Fprintf(w, "  positive control: expected %d, got %d  -> %s\n", expected, got, state)

		if state == "BROKEN" {
			fmt.Fprintf(w, "  result: NOT REPORTED. A detector that does not measure cannot say 'clean'.\n")
			continue
		}
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
	fmt.Fprintf(w, "\nfindings: %d   broken detectors: %d\n", total, broken)
	return total, broken
}
