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

### Interpreter one-liners without a read loop: heuristic reads

`python3 -c "..." file.go` and `perl -e '...' file.go` pass the operand
to the program as an argument; whether the program reads it is
undecidable statically. These operands become `ReadTarget` values
anyway, by explicit consumer decision (2026-06-12): the operand shape is
search-like in practice, and the consumer's tool policy plus its
index-aware validator bound the blast radius (an unlisted argv0 or an
unindexed path never blocks). This is a deliberate, documented exception
to the no-fabricated-facts rule for read targets; the doc comment on the
scan carries the rationale.

Covered forms: `python -c`, `python3 -c`, `perl -e`/`-E` without
`-n`/`-p`. The operands counted are the bare operands after the program
flag's value, with flag values (`-I dir`, `-m mod`, and the like) skipped
via the existing value-flag tables.

### Not covered

- Running a script file (`python script.py`, `perl script.pl`) stays
  unmodeled: executing a program is not a content search, and gating it
  would block ordinary development in indexed repos.
- `python -m module` stays unmodeled for the same reason.
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
- `python3 script.py a.go` reads nothing.
- `python3 -m json.tool a.json` reads nothing.
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
