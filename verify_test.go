package main

import (
	"bytes"
	"strings"
	"testing"
)

// run is verifyWith over a buffer: the tests read what a user would read.
func run(t *testing.T, lines []string, ds []Detector) (out string, findings, broken, measured int) {
	t.Helper()
	var b bytes.Buffer
	findings, broken, measured = verifyWith(&b, "test.md", lines, false, ds)
	return b.String(), findings, broken, measured
}

// Every detector must be able to say whether the input could exercise it. This
// is the half `Control` does not cover: the control runs against its own
// synthetic input, so it proves the check discriminates and says nothing about
// what arrived.
func TestEveryDetectorCanSayWhetherItCouldMeasure(t *testing.T) {
	for _, d := range Detectors() {
		if d.Applicable == nil {
			t.Errorf("detector %q has no applicability check, so its zero cannot be read", d.Name)
		}
		if d.Control == nil {
			t.Errorf("detector %q has no positive control", d.Name)
		}
	}
}

// A control run on an input the detector would refuse proves nothing about the
// detector. This is the same defect one level up from a dead control, and it is
// cheap to pin.
func TestEveryControlInputIsApplicable(t *testing.T) {
	for _, d := range Detectors() {
		if d.Applicable == nil {
			continue // the test above owns that failure; do not panic on top of it
		}
		input, _ := d.Control()
		if ok, why := d.Applicable(input); !ok {
			t.Errorf("detector %q runs its control on an input it would refuse (%s)", d.Name, why)
		}
	}
}

// Proven red before being trusted: a detector with no applicability check is
// refused, not quietly allowed to report zero.
func TestDetectorWithoutApplicabilityIsBroken(t *testing.T) {
	// Deliberately built on raggedColumns rather than nonASCII: a control for the
	// ascii check must contain a non-ASCII character, and that exception is
	// documented for verify.go alone. An exception that spreads to satisfy a test
	// stops being an exception.
	bare := Detector{
		Name:  "bare",
		Desc:  "a detector someone forgot to finish",
		Check: raggedColumns,
		Control: func() ([]string, int) {
			return []string{"| a | b |", "| --- | --- |", "| 1 | 2 | 3 |"}, 1
		},
	}
	out, _, broken, _ := run(t, []string{"| a | b |", "| --- | --- |", "| 1 | 2 |"}, []Detector{bare})
	if broken != 1 {
		t.Fatalf("a detector with no applicability check must be BROKEN, got broken=%d", broken)
	}
	if !strings.Contains(out, "no applicability check") {
		t.Errorf("the output does not say why it is broken:\n%s", out)
	}
	if !strings.Contains(out, "NOT REPORTED") {
		t.Errorf("a broken detector must not report a result:\n%s", out)
	}
}

// The other new way to be broken: the control itself runs on an input this
// detector would refuse, so it cannot be evidence of anything.
func TestControlOnRefusedInputIsBroken(t *testing.T) {
	crooked := Detector{
		Name:       "crooked",
		Desc:       "its control input is not one it accepts",
		Check:      raggedColumns,
		Control:    func() ([]string, int) { return []string{"no table here at all"}, 0 },
		Applicable: hasComparableTable,
	}
	out, _, broken, _ := run(t, []string{"| a | b |", "| --- | --- |", "| 1 | 2 |"}, []Detector{crooked})
	if broken != 1 {
		t.Fatalf("a control run on a refused input must be BROKEN, got broken=%d", broken)
	}
	if !strings.Contains(out, "would refuse") {
		t.Errorf("the output does not name the reason:\n%s", out)
	}
}

// The case stele's question found: an empty file used to print "0 findings",
// which is a zero about the corpus when it is a zero about the question.
func TestEmptyInputIsNotApplicableToAnyDetector(t *testing.T) {
	out, findings, broken, measured := run(t, nil, Detectors())
	if broken != 0 {
		t.Fatalf("an empty input must not break a detector, got broken=%d", broken)
	}
	if findings != 0 {
		t.Fatalf("an empty input cannot produce findings, got %d", findings)
	}
	if strings.Contains(out, "0 findings") {
		t.Errorf("an input that could not be measured must not read as clean:\n%s", out)
	}
	if !strings.Contains(out, "NOT APPLICABLE") {
		t.Errorf("the output does not say the detectors could not be exercised:\n%s", out)
	}
	if measured != 0 {
		t.Fatalf("no detector could run against an empty input, got measured=%d", measured)
	}
	if !strings.Contains(out, "cannot say clean (exit 2)") {
		t.Errorf("the summary does not carry the consequence of measuring nothing:\n%s", out)
	}
}

// The contract half: measured == 0 is what `cmdVerify` turns into exit 2. Zero
// findings out of zero detectors is not the same answer as zero findings out of
// two, and until 0.5.0 both left through the same door.
func TestMeasuredIsWhatSeparatesTheTwoZeros(t *testing.T) {
	_, findings, broken, measured := run(t, nil, Detectors())
	if findings != 0 || broken != 0 || measured != 0 {
		t.Fatalf("empty input: want 0/0/0, got findings=%d broken=%d measured=%d",
			findings, broken, measured)
	}
	// The negative control, in the same test so the two cannot drift apart: an
	// input that CAN be measured and is clean must look different.
	_, findings, broken, measured = run(t, []string{"clean ascii prose"}, Detectors())
	if findings != 0 || broken != 0 || measured != 1 {
		t.Fatalf("measurable clean input: want 0/0/1, got findings=%d broken=%d measured=%d",
			findings, broken, measured)
	}
}

// A document with prose and no table: ascii can measure it, columns cannot, and
// the two must not print the same thing.
func TestNoTableIsNotApplicableToColumnsOnly(t *testing.T) {
	out, _, _, _ := run(t, []string{"# A heading", "", "prose with no table"}, Detectors())
	if !strings.Contains(out, "the input has no table") {
		t.Errorf("columns should have refused this input:\n%s", out)
	}
	if !strings.Contains(out, "result: 0 findings") {
		t.Errorf("ascii could measure this input and should have reported a real zero:\n%s", out)
	}
}

// A header with no rows is the trap that killed the column detector under
// --last: nothing to compare against, so its zero was never about the document.
func TestHeaderWithoutRowsIsNotApplicable(t *testing.T) {
	ok, why := hasComparableTable([]string{"| a | b | c |", "| --- | --- | --- |"})
	if ok {
		t.Fatal("a table header with no rows cannot exercise the column check")
	}
	if !strings.Contains(why, "no row to compare") {
		t.Errorf("the reason should say what is missing, got %q", why)
	}
}

// The negative control for all of the above: on an input both detectors can
// measure, the change must not have suppressed real reporting.
func TestApplicableInputStillReportsFindings(t *testing.T) {
	lines := []string{"| a | b | c |", "| --- | --- | --- |", "| 1 | 2 | 3 | 4 |"}
	out, findings, broken, measured := run(t, lines, Detectors())
	if broken != 0 {
		t.Fatalf("no detector should be broken here, got %d", broken)
	}
	if findings != 1 {
		t.Fatalf("the ragged row must still be found, got %d findings", findings)
	}
	if strings.Contains(out, "NOT APPLICABLE") {
		t.Errorf("nothing should have been skipped on this input:\n%s", out)
	}
	if measured != 2 {
		t.Errorf("both detectors could measure this input, got measured=%d", measured)
	}
}
