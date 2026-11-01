package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/emezav/markgrep/search"
)

// The help text and the validator must not drift: a validated flag whose help
// lists the wrong names refuses something it never advertised. This caught the
// real cost of validating --kind -- the list had two homes and adding `html`
// updated only one.
func TestUsageAdvertisesEveryValidKind(t *testing.T) {
	for _, k := range search.Kinds {
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
	for _, k := range search.Kinds {
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
