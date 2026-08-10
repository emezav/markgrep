package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const version = "0.10.0"

// usage is built rather than written, because the list of kinds had two homes
// and they drifted: adding `html` updated Kinds and left the help text stale.
// A validated flag whose help text lists the wrong names is worse than an
// unvalidated one -- it refuses a name it does not advertise.
var usage = strings.Replace(usageTemplate, "{{kinds}}", strings.Join(Kinds, " | "), 1)

const usageTemplate = `markgrep ` + version + ` -- grep for markdown prose: searches units, not lines.

  markgrep search [-e|-f PATTERN]... [--in PATH] [flags]
  markgrep verify --file PATH [--last]

From grep, whatever translates to the unit model (where grep says "line",
read "unit"):

  -e, -f PATTERN   pattern; repeatable. Several = several phrasings of the SAME idea
  -F               fixed string, not a regular expression
  -v               units where NO phrasing matches
  -l               paths only
  -c               count only
  -r               recursive (already the default; accepted and ignored)
  -i               case insensitive (already the default; see the difference below)
  -s               case SENSITIVE
  --include GLOB   restrict by file name
  --exclude GLOB   skip by file name

Own:
  --in PATH        file or directory (default: .)
  --kind KIND[,..] {{kinds}} | any
                   one unit per table ROW; unknown names are refused, not ignored
  --links MODE     text (default) | url | both -- what a [text](url) contributes
  --raw            print the unit with its line breaks; flattened by default

ONE DELIBERATE DIFFERENCE from grep: search is case INSENSITIVE by default. On a
prose corpus that is what serves; ask for case sensitivity with -s. Said out
loud rather than hidden.

WHAT IS NOT IMPLEMENTED, and why: -A, -B, -C and -o belong to the LINE model.
Here the context IS the unit and the result IS the unit, so those flags have no
translation. They are rejected with an explanation instead of half-implemented.

Exit codes:  0 = matches   1 = no matches
             2 = error, BROKEN detector, or nothing could measure the input

It emits no verdicts: it says what is there and where it lives. The fallback is
always grep, and every output prints the equivalent command.
`

// rejected maps a line-model flag to the reason it does not exist here. The
// message is the design, not an error string: whoever hits it learns the model
// at the moment of the error, which is where a limit is worth anything.
var rejected = map[string]string{
	"-A": "-A asks for N LINES after the match. Here the context is the whole unit:\n     you already get it complete, with its heading. Use --raw to keep its line breaks.",
	"-B": "-B asks for N LINES before the match. Here the context is the whole unit.",
	"-C": "-C asks for N LINES around the match. Here the context IS the unit, which is\n     precisely why this tool exists: lines are not the boundary of meaning.",
	"-o": "-o prints only the matching fragment. Here the result IS the unit; returning\n     the fragment would mean going back to the model this tool leaves behind.",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}
	switch os.Args[1] {
	case "search":
		cmdSearch(os.Args[2:])
	case "verify":
		cmdVerify(os.Args[2:])
	case "version", "--version", "-V":
		fmt.Println("markgrep", version)
	default:
		fmt.Print(usage)
		os.Exit(0)
	}
}

// checkKinds validates a --kind spec against the known kinds.
//
// The alternative is worse than it looks: an unknown name filters every unit
// out and the run reports zero matches, which is indistinguishable from a
// corpus that genuinely does not contain the phrase. That is the exact failure
// this tool exists to remove, so it must not be the way a typo behaves.
func checkKinds(spec string) error {
	if spec == "" || spec == "any" {
		return nil
	}
	for _, k := range strings.Split(spec, ",") {
		k = strings.TrimSpace(k)
		known := k == "any"
		for _, valid := range Kinds {
			if k == valid {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("unknown --kind %q; known kinds: %s, any",
				k, strings.Join(Kinds, ", "))
		}
	}
	return nil
}

func checkLinks(mode string) error {
	switch mode {
	case "", LinksText, LinksURL, LinksBoth:
		return nil
	}
	return fmt.Errorf("unknown --links %q; use %s, %s or %s", mode, LinksText, LinksURL, LinksBoth)
}

func at(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func cmdSearch(args []string) {
	var pats []string
	root, raw := ".", false
	o := Options{Kind: "any"}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if why, no := rejected[a]; no {
			fmt.Fprintf(os.Stderr, "markgrep does not implement %s, and it is not an oversight:\n  -> %s\n", a, why)
			os.Exit(2)
		}
		switch a {
		case "-e", "-f":
			pats = append(pats, at(args, i+1))
			i++
		case "-F":
			o.Literal = true
		case "-v":
			o.Invert = true
		case "-l":
			o.FilesOnly = true
		case "-c":
			o.Count = true
		case "-r", "-R", "-i":
			// accepted with no effect: already the default behaviour
		case "-s":
			o.CaseSens = true
		case "--include":
			o.Include = at(args, i+1)
			i++
		case "--exclude":
			o.Exclude = at(args, i+1)
			i++
		case "--in":
			root = at(args, i+1)
			i++
		case "--kind":
			o.Kind = at(args, i+1)
			i++
		case "--links":
			o.Links = at(args, i+1)
			i++
		case "--raw":
			raw = true
		default:
			if !strings.HasPrefix(a, "-") {
				pats = append(pats, a)
			} else {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n%s", a, usage)
				os.Exit(2)
			}
		}
	}
	if len(pats) == 0 {
		fmt.Fprintln(os.Stderr, "at least one pattern is required (-e/-f)")
		os.Exit(2)
	}
	// A name we do not know would filter everything out and report zero, which
	// reads exactly like "there is nothing there". Refuse instead.
	if err := checkKinds(o.Kind); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := checkLinks(o.Links); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	units, files, err := Collect(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(2)
	}
	units, files = filterFiles(units, files, o)

	if !o.Count && !o.FilesOnly {
		fmt.Printf("corpus: %d files, %d units\n", len(files), len(units))
	}
	ms, err := Search(units, pats, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	PrintMatches(os.Stdout, ms, pats, root, raw, o)
	if len(ms) == 0 {
		os.Exit(1)
	}
}

func filterFiles(u []Unit, f []string, o Options) ([]Unit, []string) {
	if o.Include == "" && o.Exclude == "" {
		return u, f
	}
	ok := func(path string) bool {
		base := filepath.Base(path)
		if o.Include != "" {
			if m, _ := filepath.Match(o.Include, base); !m {
				return false
			}
		}
		if o.Exclude != "" {
			if m, _ := filepath.Match(o.Exclude, base); m {
				return false
			}
		}
		return true
	}
	var uu []Unit
	for _, x := range u {
		if ok(x.Path) {
			uu = append(uu, x)
		}
	}
	var ff []string
	for _, x := range f {
		if ok(x) {
			ff = append(ff, x)
		}
	}
	return uu, ff
}

func cmdVerify(args []string) {
	path := ""
	last := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--file":
			path = at(args, i+1)
			i++
		case "--last":
			last = true
		default:
			if !strings.HasPrefix(args[i], "-") && path == "" {
				path = args[i]
			}
		}
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "--file is required")
		os.Exit(2)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read error:", err)
		os.Exit(2)
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}

	findings, broken, measured := Verify(os.Stdout, path, lines, last)
	switch {
	// Two zeros that read alike from the outside: nothing wrong, and nothing
	// looked at. A caller scripting on the exit code was told "clean" for a
	// corpus that could not have contained a finding, which is the false
	// absence this tool exists to remove. Both mean "you did not get an
	// answer", so both are 2.
	case broken > 0 || measured == 0:
		os.Exit(2)
	case findings > 0:
		os.Exit(1)
	}
}
