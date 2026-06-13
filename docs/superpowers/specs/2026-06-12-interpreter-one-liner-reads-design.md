# Interpreter program read analysis

Date: 2026-06-12
Status: approved
Increment: 1 of 4 (python). awk, sed, perl are named follow-on increments.

## Background

shelldecomp parses a shell command and reports the files it reads as
`ReadTarget` values. A consumer gates on those reads; agent-gate's
code-search rule blocks a read of an lm-semantic-search-indexed codebase
and redirects the agent to semantic search. The rule's tool policy lives
in consumer config (`search_tools`), so shelldecomp emitting a read for a
program never blocks anything on its own: the consumer must also list
that program.

shelldecomp already understands code search embedded in shell: a
`bash -c "grep ..."`, a `find ... -exec grep`, and a heredoc temp script
all surface their inner reads because shelldecomp parses the embedded
shell and reads the real command operands.

The gap this increment closes is interpreter programs. When an agent runs
`python -c "..." file`, `python script.py file`, or a script that opens
an indexed file by a path written inside the script, none of those reads
are visible today. The program's behavior is either inline text the
parser does not interpret, or a file on disk the parser never opens.

A prior draft modeled every operand after the program as a blind read.
That was rejected: it asserts a read without understanding the program,
which a regex could do and which defeats the point of a syntax-aware
decomposer. This design instead understands the program.

## Scope

All four interpreter languages are in scope: python, awk, sed, and perl,
including `python -m`. They ship as a sequence of commits, not because
any is optional, but because they have a hard dependency order. python is
built first because its grammar is already a pinned dependency and it
carries the shared machinery (the resolver seam and the analyzer
registry) that the other three reuse unchanged. awk, sed, and perl each
then add one grammar following the dart and swift submodule precedent
plus one analyzer. The build order is python, awk, sed, perl, by grammar
onboarding cost; all four are delivered as part of this effort.

## Architecture

The per-language read analysis lives in shelldecomp, where the grammars
and parse trees already are. The one new impure capability, reading a
referenced script off disk, is an injected callback so shelldecomp stays
a pure, testable parser and the consumer owns filesystem policy.

### Resolver seam

shelldecomp gains an optional file resolver:

    type FileResolver func(absPath string) (content []byte, ok bool)

threaded through a new `ParseWithOptions(command, baseCwd, Options)`
entry point. `Parse(command, cwd, home)` is unchanged and passes no
resolver, so existing behavior is identical. A resolver that returns
`ok=false` (missing, too big, unreadable) contributes nothing; no read
is fabricated. A nil resolver disables off-disk reads entirely.

### Analyzer registry

A registry maps a `Lang` to a `ReadAnalyzer`:

    type ReadAnalyzer func(AnalyzerInput) (reads []ReadTarget, writes []WriteTarget)

`AnalyzerInput` carries the parsed tree root, the source bytes, the
program's argv operands, the cwd, the home dir, the resolver, a visited
set, and the remaining depth. The analyzer runs over the live
tree-sitter tree inside `parseForeign`, populating the returned
decomposition's reads and writes. A language with no registered analyzer
behaves exactly as today.

### One analyzer, two sources

A python program reaches the analyzer the same way whether it is inline
or on disk. For `python -c "code"`, the embedding text is `code`. For
`python script.py`, shelldecomp resolves `script.py` through the resolver
and the embedding text is the file's bytes. The trailing operands become
the program's argv in both cases, so `sys.argv` maps correctly either
way.

## Python analyzer

Detected reads: `open(p)` in read mode, `io.open(p)`,
`pathlib.Path(p).read_text()` / `.read_bytes()`, `Path(p).open()` in read
mode, and `fileinput.input([...])` or a bare `fileinput.input()`.

Detected writes: `open(p, "w"|"a"|"x"|"...+")`,
`Path(p).write_text()` / `.write_bytes()`, `Path(p).open()` in write
mode. A write is not a search, so the consumer's write guard drops it.

Subprocess recursion: `subprocess.run` / `call` / `check_call` /
`check_output` / `Popen([list])` and `os.system(str)` whose program is a
declared searcher or interpreter recurse back through shelldecomp, so
`python -c "import os; os.system('grep -rn X /repo')"` surfaces /repo.

Path argument resolution: a string literal resolves against the cwd;
`sys.argv[N]` or `argv[N]` maps to the Nth program operand;
`sys.argv[1:]` or a no-arg `fileinput.input()` means all operands. An
f-string or any expression the analyzer cannot resolve to a literal or an
argv slot yields no read. The analyzer never fabricates a path.

## Recursion, cycles, limits

The existing depth budget bounds nesting (python runs a script that
shells out to grep, and so on). A visited set keyed by resolved absolute
script path stops a script-runs-itself cycle. The resolver enforces
existence and a size cap, so the hot path cannot be dragged into reading
a huge or absent file.

## Consumer integration (agent-gate)

agent-gate supplies a disk resolver that reads a path only when it
exists, is a regular file, and is under a size cap, and threads it into
`ExtractCodeSearchTargets`. python region reads are folded into the
existing embedded-recursion path when `python`/`python3` is in the rule's
`search_tools`. The whole decision stays memoized by the existing exec
verdict cache.

## Testing

shelldecomp table tests with a fake in-memory resolver cover inline `-c`,
off-disk script (hardcoded path, argv mapping, relative path, resolver
miss, nil resolver), writes, pathlib and fileinput, subprocess to a
searcher, and a self-referential cycle. Existing python, perl, node,
ruby, and awk suites stay green. agent-gate adds resolver-limit tests and
live probes after the submodule pin bump.

## Delivery sequence (all in scope)

Every item below is committed work in this effort, ordered by dependency
and grammar-onboarding cost.

1. python (this increment): the resolver seam, the analyzer registry, and
   the python analyzer, including `python -m`. For `-m`, the command-line
   operands are analyzed like any interpreter, and when the module name
   resolves to a local file under the cwd, its body is read through the
   resolver and analyzed the same as a script file; a module that resolves
   only into installed site-packages is not a search of the repo and
   yields no read.
2. awk: add the Beaglefoot grammar as a submodule (dart pattern, committed
   parser and scanner), add the awk analyzer for `getline < file` reads
   and `print > file` writes inside the script, and fold in the
   `gawk -i inplace` read-scan fix.
3. sed: add the mskelton grammar as a submodule (committed parser, no
   external scanner), add the sed analyzer for the `r file` read and
   `w file` write commands inside the script.
4. perl: add the tree-sitter-perl grammar as a submodule (swift pattern,
   generate the parser, reuse the committed scanner), add the perl
   analyzer, and fold in the `-n`/`-p` flag-fact reads.

Reading the body of a python module imported with `import` (as opposed to
run with `-m`) stays out of scope: that is arbitrary dependency
resolution, not a command the agent issued.
