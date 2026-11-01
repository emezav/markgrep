# markgrep

**grep for markdown prose.** It searches semantic units -- paragraphs, list items,
sections -- instead of lines, and gives you back the whole unit with the heading it
lives under.

It emits no verdicts. It tells you what is there and where it lives.

## The problem

`grep` works on lines. Prose is hard-wrapped. So a phrase longer than three or four
words may be split by a newline, and **a correct query returns nothing** -- a failure
that reads exactly like an absence.

```console
$ grep -c "Rule of thumb: do not read a" NOTES.md
0                       # the phrase is there; the wrap split it

$ markgrep search -F -e "Rule of thumb: do not read a" --in NOTES.md
NOTES.md:32  [paragraph]  ## Reading strategy
  | **Rule of thumb:** do not read a whole file "just in case"; use the map and
  | search by range.
```

There is a second failure, subtler, and it is the one that made this tool
markdown-specific rather than prose-generic:

> **Inline emphasis does not wrap words, it wraps arbitrary spans** -- punctuation
> included. A document may contain `**Rule of thumb:**` with the colon *inside* the
> asterisks, so whoever writes the query cannot know where the author put them.
> Hard wrapping is at least predictable. This is not.

`markgrep` strips inline markup from the text before matching, so you write the
phrase as you would say it.

## Install

```console
go install github.com/emezav/markgrep@latest
```

Or build from source with `go build`. No dependencies beyond the standard library,
and the result is a single binary.

### Update an existing install

Run the same command again. `go install` overwrites the binary in `$(go env GOPATH)/bin`,
and with CMake you rebuild and `cmake --install` over the same `--prefix`.

**Do not use `markgrep version` to decide whether you need to.** It prints the version
compiled into the source, so any fix that does not raise that number leaves the string
identical before and after -- which is exactly what happened to the release that made
this help text ASCII. A version string is a claim about the source, not a fingerprint
of the binary.

The fingerprint is there, though, because `go install` stamps it in:

```console
go version -m $(command -v markgrep) | grep vcs.
```

That prints the exact `vcs.revision` your binary was built from, its `vcs.time`, and
`vcs.modified` if it came from a dirty tree. Compare it against `git rev-parse HEAD`,
or against the repository, and you have an answer instead of an inference.

There are no release tags, so `@latest` resolves to the newest commit on `main` as a
pseudo-version.

### Build and install with CMake

There is no C in this project: CMake is a portable front end over `go build`, so the
same commands configure, test, install and package on **Windows and Linux alike**.

```console
cmake -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
ctest --test-dir build
cmake --install build --prefix ~/.local
```

The binary lands in `<prefix>/bin`, the README and LICENSE in
`<prefix>/share/doc/markgrep`. Omitting `--prefix` uses the system default
(`/usr/local` on Linux, `C:\Program Files\markgrep` on Windows, which needs an
elevated shell). `cmake --build build --target uninstall` removes exactly what was
installed.

**On Windows the default generator is Visual Studio, which is multi-config:** add
`--config Release` to the build, test, install and package commands. With Ninja or
Makefiles that flag is unnecessary, since `CMAKE_BUILD_TYPE` already decided.

Presets cover the usual combinations, if you prefer them:

```console
cmake --preset release && cmake --build --preset release && ctest --preset release
```

| Option | Default | Meaning |
| --- | --- | --- |
| `MARKGREP_STRIP` | ON, OFF in Debug | Drop symbol and DWARF tables (`-ldflags "-s -w"`) |
| `MARKGREP_TRIMPATH` | ON | Keep local paths out of the binary (`-trimpath`) |
| `MARKGREP_BUILD_TESTS` | ON | Register `go vet`, `go test` and the CLI smoke tests |
| `MARKGREP_INSTALL_DOCS` | ON | Install README.md and LICENSE |
| `MARKGREP_GOOS` / `MARKGREP_GOARCH` | host | Cross-compilation target |
| `GO_EXECUTABLE` | found in PATH | Which Go toolchain to use |

Cross-compiling needs no extra toolchain, because `CGO_ENABLED=0` keeps the binary
pure Go. The `.exe` suffix follows the target, not the host, and the CLI tests are
skipped when the result cannot run locally:

```console
cmake -B build-linux -DMARKGREP_GOOS=linux -DMARKGREP_GOARCH=amd64
cmake --build build-linux
cpack --config build-linux/CPackConfig.cmake   # -> markgrep-0.1.0-linux-amd64.tar.gz
```

`cpack` writes a ZIP on Windows and a `.tar.gz` elsewhere, into the build directory,
containing the same tree `cmake --install` would produce.

The version is read out of `main.go` at configure time, so the binary, the install
tree and the archive names cannot drift apart.

## Use

```console
markgrep search [-e|-f PATTERN]... [--in PATH] [flags]
markgrep verify --file PATH [--last]
```

**Several patterns mean several phrasings of the same idea.** They are grouped on
purpose: the question being answered is *where does this idea live*, not *how many
times does this string occur*.

```console
markgrep search -e "do not record state" -e "unobservable state" --in docs/
```

### Flags borrowed from grep

Where grep says *line*, read *unit*: `-e`, `-f`, `-F`, `-v`, `-l`, `-c`, `-i`, `-r`,
`--include`, `--exclude`. Regular expressions work as you expect.

`--include` and `--exclude` take a glob over the file's base name and **accumulate**, as
in grep: `--exclude a.md --exclude b.md` leaves both out, and a file passes if it matches
any `--include`. **A glob that matches no file, or is malformed, exits 2** and names
it, because a filter that filters nothing reads exactly like one that worked.

**Nothing to search is refused too, with exit 2 and not 1.** A root with no markdown, an
empty file, or filters that each match something and together leave nothing all end with
zero units -- and zero matches over zero units reads exactly like a corpus that lacks the
phrase. The same goes for a `--kind` that matches none of the units left:

```console
$ markgrep search -F -e "rule of thumb" --in docs/ --include a.md --exclude a.md
0 files, 0 units under docs/ after the filters: nothing to search
$ markgrep search -F -e "rule of thumb" --in docs/ --kind table
--kind "table" matches none of the 40 units in 3 files: filter matches nothing
```

**One deliberate difference:** search is **case insensitive by default**. On a prose
corpus that is what serves; ask for sensitivity with `-s`.

### Flags of its own

| Flag | What it does |
| --- | --- |
| `--in PATH` | File or directory; default `.` |
| `--kind KIND[,...]` | Restrict to one or more unit kinds: `paragraph`, `item`, `code`, `table`, `heading`, `quote`, `frontmatter`, `html`, or `any` |
| `--links MODE` | What a `[text](url)` contributes to the searchable form: `text` (default), `url`, or `both` |
| `--raw` | Print the unit with its line breaks instead of flattened |
| `--control PHRASE` | A phrase you know is in the corpus; repeatable. If it matches nothing, the whole search fails with exit 2 and reports nothing |

**A name it does not know is refused, not ignored.** `--kind paragrph` exits 2 with the
list of valid kinds, because filtering everything out and reporting zero is
indistinguishable from a corpus that genuinely lacks the phrase -- which is the exact
failure this tool exists to remove.

**Links resolve to their visible text by default**, so a phrase flows across them:

```console
$ markgrep search -F -e "the whole idea continues" --in notes.md
notes.md:12  [paragraph]  ## Notes
  | text with the whole idea continues past the link
```

Without that, the URL sits between "idea" and "continues" and the query returns
nothing -- the same false absence a hard wrap produces. Ask for the URLs with
`--links url`, or for both with `--links both`.

**A zero is worth what its control is worth.** `--control` runs a phrase you know is
there with the same options and the same filters as the search (`-v` aside, which would
make any corpus pass). If the corpus cannot find it, a zero from the real search says
nothing about absence, so the search refuses to report one:

```console
$ markgrep search -F -e "rule of thumb" --control "known phrase" --in docs/ --kind item
--control "known phrase" matched 0 of N units in M files with the same options; nothing is reported: positive control matched nothing
```

`N` and `M` are the corpus after the filters, so the refusal says which corpus failed to
answer.

### Flags deliberately not implemented

`-A`, `-B`, `-C` and `-o` belong to the line model. Here **the context is the unit
and the result is the unit**, so they have no translation. They are rejected with an
explanation rather than half-implemented:

```console
$ markgrep search -C 3 -e "anything" --in docs/
markgrep does not implement -C, and it is not an oversight:
  -> -C asks for N LINES around the match. Here the context IS the unit, which is
     precisely why this tool exists: lines are not the boundary of meaning.
```

## grep is the fallback, and every output says so

Each result prints the equivalent `grep` command. If you do not have `markgrep`
installed you can reproduce the search -- worse, but you can. And if you have it, you
can drop it without losing anything.

That is deliberate: **a tool you cannot walk away from is a dependency, not a
convenience.**

The command is meant to be pasted, so it is emitted as **ERE** with the pattern and
the path quoted for a shell, and a literal (`-F`) is escaped into ERE rather than
declared with `-F`. Two reasons, both found by going to run it:

- `grep -i` together with `-F` **aborts** in a single-byte locale (`C`, `POSIX`, or
  none set) on at least one GNU grep 3.0 build -- whatever the pattern, whatever the
  input, even from standard input. Under a UTF-8 locale it works.
- A regular expression handed to `grep` with no dialect flag is read as **BRE**, where
  alternation is a literal character. For `unit|line` markgrep matched three units and
  the command it printed matched one -- not a weaker reproduction, a different question.

The file filters travel with it as `--include=` and `--exclude=`, which GNU grep
reads the same way, so a filtered search does not print a command over the whole tree.

**`-c` and `-l` do not print it**, and that is a decision rather than an oversight:
they exist to be piped, and a caller counting lines should get lines. Run the search
without them to get the command.

## Emphasis is removed before matching, and that can look like a bug

`markgrep` matches against the flattened unit, and flattening removes inline emphasis -- asterisk,
backtick and tilde. That is the point: a `**bold**` in the middle of a sentence would otherwise split
a phrase nobody wrote as split.

The consequence is worth knowing before it costs you an afternoon:

```console
$ markgrep search -e '^- item' --kind item --in notes.md   # every item
$ markgrep search -e '^- \*item' --kind item --in notes.md  # nothing
```

The **list marker is kept** and the asterisks are not, so a pattern copied out of `--raw` or out of
the source does not match in regex mode. Under `-F` it does, because the pattern is normalized the
same way. An empty result whose pattern carries one of those marks says so.

The underscore is **not** removed: `_emph_` survives where `*emph*` does not, because underscores
belong to identifiers like `audit_log`.

## What it will not do

- **No verdicts.** It never says "this is orphaned" or "this is clean".
- **No writing.** It does not fix, rewrite or touch your documents.
- **No index, no cache, no config, no network, no model.**

That last one is a positioning decision, not austerity. The intended caller --
an agent working through its own corpus -- **already has semantic judgment**. What it
lacks is text arriving in the right units, with its section. Embedding a model here
would duplicate the one invoking it.

## Prior art

[`paragrep`](https://github.com/bmc/paragrep) has been solving the paragraph half of
this since 1989 (C, then Perl, now Python), and solves parts this does not: logical
AND/OR across patterns, configurable paragraph delimiters. **If you need
paragraph-grep over plain text, use it.**

What `markgrep` adds is markdown awareness: the **section heading** a match lives
under, **inline markup normalization**, and unit kinds (item, table, code, quote).
Those matter on a corpus written for agents to read, which is a 2026 problem, not
a 1989 one.

## Use it as a library

The unit model and the search are a package, so a program can search markdown by unit
without a shell between it and the pattern -- the text most likely to lose a backslash on
the way:

```go
import "github.com/emezav/markgrep/search"

r, err := search.Run("docs/", []string{"rule of thumb"}, search.Options{
    Literal: true,
    Exclude: []string{"CHANGELOG.md"},
    Control: []string{"known phrase"},
})
// r.Files and r.Units: the corpus after the filters.
// r.Matches: each unit with its path, line, kind, heading and text.
//   U.Heading keeps an ATX heading's "#" marks ("# memory"); U.Raw() is the text as
//   written, while matching runs on the flattened form (U.FlatWith).
// r.Grep: the equivalent grep command, one per phrasing.
```

Every refusal is an error, never an empty result: an unknown kind, a malformed or dead
glob or a `--kind` with no unit of its kind (`errors.Is(err,
search.ErrFilterMatchesNothing)`), nothing left to search (`search.ErrEmptyCorpus`), a
control that found nothing (`search.ErrControlFailed`). Pinning the module version in your `go.mod` records which
markgrep you search with. The CLI is a thin layer over `Run`.

## Roadmap

Markdown awareness is the direction of the project, stated plainly rather than
implied. What has landed:

- **setext headings** (`Title` over `=====` or `-----`). Before this, a document
  written that way reported `(no section)` for every match -- the headline feature
  failing silently on a whole syntax.
- **links**: match the text, the URL, or both (`--links`), so a phrase crosses a link
  the way it crosses a hard wrap.
- **YAML front matter** as its own kind, so metadata stops arriving as prose.
- **indented code blocks** typed as `code` instead of parsed as paragraphs.

- **a table row is a unit**, so a hit in a forty-row table returns that row and not
  the other thirty-nine. The separator row carries no content and is dropped.
  **Deviation from what this roadmap used to promise**, and it is deliberate: it said
  *cells*, and a cell is rarely a unit of meaning while a row is the record. Cell
  granularity would also break phrases that read across a row.
- **embedded HTML**: block elements and comments are their own kind, `html`, so hidden
  markup stops arriving as prose; and inline tags no longer split a phrase --
  `an <b>important</b> idea` matches `an important idea`, the same way a link does.
  A bare `<` in prose (`a < b`) is left alone.
- **inline code versus fenced blocks** is settled: inline code lives inside its
  paragraph with the backticks stripped, while fenced *and* four-space indented blocks
  are both `--kind code`.

What is still open:

- **whether a fenced block should be flattened at all.** Inside a fence a newline
  carries meaning, so a phrase crossing two lines of a command arguably does not
  exist. Today it matches. Changing it would change what `--kind code` returns, so it
  is a contract decision waiting on a real use case, not a fix.

## Design notes

These invariants hold the `verify` side together, every one of them learned the
hard way:

- **Every detector carries its positive control**, and result and control are
  printed **in the same output**. A "clean" must never be readable without seeing
  that the detector was measuring anything.
- **The scope narrows what is reported, never what the detector reads.** Trimming
  the input looks equivalent and is not -- it turns a healthy detector into a dead
  one, silently.
- **Every detector also says whether the input could have exercised it at all.**
  A control proves the question is well posed; it cannot prove the corpus could
  answer it. So an input with no table reports `NOT APPLICABLE -- the input has no
  table` rather than `0 findings`, because a zero about the question reads exactly
  like a zero about the document.
- **And the exit code says it too.** If *nothing* could measure the input,
  `verify` exits 2, not 0. Otherwise a caller scripting on the exit code is told a
  corpus that could not contain a finding is clean -- which is the same false
  absence, one layer down where no one is reading the output.

## License

MIT
