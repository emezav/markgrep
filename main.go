package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/emezav/markgrep/search"
)

const version = "0.13.0"

// usage is built rather than written, because the list of kinds had two homes
// and they drifted: adding `html` updated Kinds and left the help text stale.
// A validated flag whose help text lists the wrong names is worse than an
// unvalidated one -- it refuses a name it does not advertise.
var usage = strings.Replace(usageTemplate, "{{kinds}}", strings.Join(search.Kinds, " | "), 1)

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
  --include GLOB   restrict by file name; repeatable, a file passes if it matches any
  --exclude GLOB   skip by file name; repeatable
                   a glob that matches no file, or is malformed, is an error (exit 2)

Own:
  --in PATH        file or directory (default: .)
  --kind KIND[,..] {{kinds}} | any
                   one unit per table ROW; unknown names are refused, not ignored,
                   and so is a kind that matches none of the units (exit 2)
  --links MODE     text (default) | url | both -- what a [text](url) contributes
  --raw            print the unit with its line breaks; flattened by default
  --control PHRASE a phrase known to be in the corpus; repeatable. Matched with the
                   same options (not -v). If one matches nothing, the search fails
                   with exit 2 and reports nothing

INLINE EMPHASIS IS REMOVED BEFORE MATCHING -- asterisk, backtick and tilde -- so a
pattern copied from --raw or from the source does not match in regex mode:
"^- *item" finds nothing where "^- item" finds every item, because the LIST MARKER
is kept and the asterisks are not. -F normalizes the pattern the same way, so
under -F the copied text does match. The underscore is NOT removed: _emph_
survives where *emph* does not, because underscores belong to identifiers.

ONE DELIBERATE DIFFERENCE from grep: search is case INSENSITIVE by default. On a
prose corpus that is what serves; ask for case sensitivity with -s. Said out
loud rather than hidden.

WHAT IS NOT IMPLEMENTED, and why: -A, -B, -C and -o belong to the LINE model.
Here the context IS the unit and the result IS the unit, so those flags have no
translation. They are rejected with an explanation instead of half-implemented.

Exit codes:  0 = matches   1 = no matches
             2 = error, BROKEN detector, nothing could measure the input,
                 a filter that filters nothing, nothing left to search,
                 or a failed --control

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

func at(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func cmdSearch(args []string) {
	var pats []string
	root := "."
	var out Output
	o := search.Options{Kind: "any"}

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
			out.FilesOnly = true
		case "-c":
			out.Count = true
		case "-r", "-R", "-i":
			// accepted with no effect: already the default behaviour
		case "-s":
			o.CaseSens = true
		case "--include":
			o.Include = append(o.Include, at(args, i+1))
			i++
		case "--exclude":
			o.Exclude = append(o.Exclude, at(args, i+1))
			i++
		case "--control":
			o.Control = append(o.Control, at(args, i+1))
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
			out.Raw = true
		default:
			if !strings.HasPrefix(a, "-") {
				pats = append(pats, a)
			} else {
				fmt.Fprintf(os.Stderr, "unknown flag: %s\n\n%s", a, usage)
				os.Exit(2)
			}
		}
	}

	// Every refusal -- an unknown or dead kind, a malformed or dead glob,
	// nothing left to search, a control that found nothing -- is exit 2, because each of them would otherwise
	// have reported a zero that reads exactly like "there is nothing there".
	r, err := search.Run(root, pats, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if !out.Count && !out.FilesOnly {
		fmt.Printf("corpus: %d files, %d units\n", len(r.Files), r.Units)
	}
	PrintMatches(os.Stdout, r, pats, root, out, o)
	if len(r.Matches) == 0 {
		os.Exit(1)
	}
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
