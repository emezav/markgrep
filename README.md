# markgrep

**grep for markdown prose.** It searches semantic units — paragraphs, list items,
sections — instead of lines, and gives you back the whole unit with the heading it
lives under.

It emits no verdicts. It tells you what is there and where it lives.

## The problem

`grep` works on lines. Prose is hard-wrapped. So a phrase longer than three or four
words may be split by a newline, and **a correct query returns nothing** — a failure
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

> **Inline emphasis does not wrap words, it wraps arbitrary spans** — punctuation
> included. A document may contain `**Rule of thumb:**` with the colon *inside* the
> asterisks, so whoever writes the query cannot know where the author put them.
> Hard wrapping is at least predictable. This is not.

`markgrep` strips inline markup from the text before matching, so you write the
phrase as you would say it.

## Install

```console
go install github.com/emezav/markgrep@latest
```

Or build from source: `go build`. No dependencies beyond the standard library, and
the result is a single binary.

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

**One deliberate difference:** search is **case insensitive by default**. On a prose
corpus that is what serves; ask for sensitivity with `-s`.

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
installed you can reproduce the search — worse, but you can. And if you have it, you
can drop it without losing anything.

That is deliberate: **a tool you cannot walk away from is a dependency, not a
convenience.**

## What it will not do

- **No verdicts.** It never says "this is orphaned" or "this is clean".
- **No writing.** It does not fix, rewrite or touch your documents.
- **No index, no cache, no config, no network, no model.**

That last one is a positioning decision, not austerity. The intended caller —
an agent working through its own corpus — **already has semantic judgment**. What it
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

## Roadmap

Markdown awareness is **shallow today** and that is the direction of the project,
stated plainly rather than implied:

- links: match the text, the URL, or both — today neither is handled specially
- inline code versus fenced blocks
- YAML front matter
- table cells as units of their own
- embedded HTML

Until those land, "markdown-aware" means *heading, markup and unit kind*, and
nothing more.

## Design notes

Two invariants hold the `verify` side together, both learned the hard way:

1. **Every detector carries its positive control**, and result and control are
   printed **in the same output**. A "clean" must never be readable without seeing
   that the detector was measuring anything.
2. **The scope narrows what is reported, never what the detector reads.** Trimming
   the input looks equivalent and is not — it turns a healthy detector into a dead
   one, silently.

## License

MIT
