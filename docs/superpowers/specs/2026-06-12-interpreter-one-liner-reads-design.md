# Interpreter one-liner read targets

Date: 2026-06-12
Status: draft, awaiting review

## Background

shelldecomp parses a shell command and reports the files it reads as
`ReadTarget` values. Consumers gate on those reads; agent-gate's
code-search rule blocks a read of an indexed codebase and redirects the
agent to semantic search. The rule's tool policy lives in consumer
config (`search_tools`), so shelldecomp emitting a read for a tool never
blocks anything by itself: the consumer must also list that tool.

Two gaps exist today.

Interpreter one-liners are not modeled. `perl -ne 'print if /x/' file.go`
and `python3 -c "..." file.go` produce no read targets, so a consumer
cannot gate them. Their inline programs are already extracted as
embedded regions; only the file operands are missing.

The gawk in-place flag corrupts the read scan. For
`gawk -i inplace '{...}' file.go`, the read scan consumes `inplace` as
the program operand and then reports the real program `'{...}'` as a
read path, fabricating a path that does not exist. (The write side
handles `-i inplace` correctly.)

## Design

### perl with a read loop: guaranteed reads

`perl -n` and `perl -p` wrap the program in a read loop over the file
operands; reading them is documented flag semantics, not a guess. When a
perl command carries `-n` or `-p` (alone or in a cluster such as `-ne`,
`-pe`, `-lne`) together with a program from `-e`/`-E`, every remaining
bare operand is a data file and becomes a `ReadTarget` with Argv0
`perl`.

### Interpreter operands without a read loop: heuristic reads

`python3 -c "..." file.go`, `perl -e '...' file.go`,
`python3 -m mod file.go`, and `python3 script.py file.go` all pass the
trailing operands to a program as arguments; whether the program reads
them is undecidable statically. These operands become `ReadTarget`
values anyway, by explicit consumer decision (2026-06-12): the operand
shape is search-like in practice, and the consumer's tool policy plus its
index-aware validator bound the blast radius (an unlisted argv0 or an
unindexed path never blocks). This is a deliberate, documented exception
to the no-fabricated-facts rule for read targets; the doc comment on the
scan carries the rationale.

The program slot, not a read target, is whichever comes first: the value
of `-c`/`-e`/`-E`/`-m`, or the first bare operand (a script file like
`script.py`). Every bare operand after the program slot is a data-file
read target. Flag values (`-I dir`, and the like) are skipped via the
existing value-flag tables. The script file in `python script.py` is the
program being executed, so it is not itself a read target; its arguments
are.

Covered argv0 values: `python`, `python3`, `perl`. The consumer lists
these in `search_tools` to opt in; gksyntax emitting the read never
blocks on its own.

### Not covered

- The contents of an executed script or module. `python script.py` with
  no operand, or a script that hardcodes an indexed path internally, is
  not gated: the code lives in a file on disk, not in the command text,
  so static command decomposition cannot see it. Reading a referenced
  script off disk and parsing it is a separate, larger design (filesystem
  reads on the hot path, missing files, symlinks, size limits) tracked as
  a follow-up spec, not built here.
- ruby, node, and other interpreters stay unmodeled until asked for.

### gawk -i inplace fix

`-i` joins the awk/gawk value-flag table in the read scan so `inplace`
is consumed as the flag's value, the real program is consumed as the
program operand, and the file operand is reported once. Under
`-i inplace` the file is also a write target, which lets a write-guard
consumer treat it as an edit rather than a search; without the flag the
file is a plain read.

## Testing

Table tests in shelldecomp following the existing read-scan patterns:

- `perl -ne 'print if /x/' a.go b.go` reads both operands.
- `perl -lne '...' a.go` (cluster) reads the operand.
- `perl -e '...' a.go` reads the operand (heuristic case).
- `perl -e '...'` with no operand reads nothing.
- `python3 -c "..." a.go` reads the operand.
- `python3 -c "..."` alone reads nothing.
- `python3 script.py a.go` reads a.go; `script.py` is the program slot,
  not a read target.
- `python3 script.py` with no operand reads nothing.
- `python3 -m mod a.go` reads a.go; `mod` is the program slot.
- `python3 -m mod` with no operand reads nothing.
- `perl -I lib -ne '...' a.go` skips the -I value and reads a.go.
- `gawk -i inplace '{...}' a.go`: a.go is both read and write, and the
  program text is not a path.
- `awk '/x/' a.go` keeps its current behavior (regression guard).

agent-gate gains follow-up probe tests after the pin bump, plus
`search_tools` config additions ("perl", "python", "python3"), in its
own repo and review.

## Rollout

1. This spec is reviewed in gksyntax.
2. TDD on this branch; `make check` green; push; review.
3. agent-gate bumps the submodule pin in its own commit with probe
   tests, config update, deploy, and live verification.
