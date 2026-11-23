# Documenting

The standard is the `instruction-writing:writing-project-docs` skill. Below is
this repository's delta, which wins where the two disagree.

## Four trees, four owners

| Tree | Owns | Register |
|---|---|---|
| `docs/*.md` | How to operate on this repository now | Command register: imperative, one instruction per line |
| `docs/decisions/` | One decision in force per file: the choice, what forced it, and what it rejected | The standard's record: at most 8 lines |
| `docs/design/` | What the system is intended to be, and why | Prose. A design doc argues; rationale is its content, not padding. |
| `docs/user-guide/` | How to use revier as it is built: every command, file, field and key a user meets | Task prose, second person: "to do X, do Y". |

`docs/design/README.md` states which design document owns what. Design docs are
not a record of what is built — the code is that record — so a design doc
describing something absent from `internal/` is intent, not documentation drift.

A design doc holds the model, its boundaries, scope and non-goals, and the
reasons. Keys, flags, numbers, field names, widths and Go code belong to the
user guide or the code; write them there, not in `product.md`,
`architecture.md` or `extending.md`.

A design doc states the model as a whole. A record states one choice and what
it rejected. A design doc cites a record and does not repeat its argument.

## `docs/decisions/`

The standard owns the record's format, its three tests and its lifecycle.

- Take no number below 0202. The numbers up to 0201 belonged to the old log,
  and the standard's next-number command does not see them.
- Before a merge, check that no other branch took the record's number: two
  files with one number merge without a conflict. When the number changes,
  change it in the commit message, the PR title and the PR body too.
- Leave what describes an adapter or the core from the inside in the Go
  comment beside it: no design doc owns it.
- When a record is withdrawn and nothing replaces it, delete it and remove
  every citation of it in the tree. A Go comment that cited it keeps its own
  words, with no number.
- A `Dnn` in an older commit or PR names an entry of the old log. Read it with
  `git show "$(git log -1 --diff-filter=D --format=%H -- docs/design/decisions.md)^:docs/design/decisions.md"`.

## `docs/user-guide/`

- Write for someone who installed revier and never opens this repository.
  Keep it short: a page says how to do a thing, with one example. Leave out
  what `revier doctor`, `--help` or an error message tells the user when it
  happens.
- Link to no file of this repository outside `docs/user-guide/`. A link to
  another site, such as a tool to install, is fine.
- Name a file path or a configuration key only where the user opens or sets
  it. A source path, a Go symbol, a `mise` task, a test layer, CI and release
  detail stay in `docs/`. The one exception is the identity the user passes
  to verify a download.
- Cite no decision. Where the user needs the reason, give it in one sentence;
  the argument stays in `docs/design/` and `docs/decisions/`.
- `cli.md` names every command with a one-line purpose. Flag detail stays with
  `--help`, except a flag a task page needs.
- `configuration.md` and `extending/` are the configuration reference. The
  binary embeds them, and `revier assist --reference` prints them, so a rule
  or field of the format is written there and nowhere else. Another page
  shows at most an example and links to the reference section.

## Conventions

- Cite a record as `(decisions/NNNN)` in `docs/`, outside `docs/user-guide/`,
  and in a Go comment as [REVIEWING.md](REVIEWING.md#quality-rules) says.
- Write a `CHANGELOG.md` section as
  [RELEASING.md](RELEASING.md#what-goes-in-the-changelog-section) says.
- No check runs on Markdown: `mise run quality` and CI test only Go. Check
  links and anchors by hand.

## README scope

`README.md` describes what revier is, how to install a release, a short first
use, and the two commands that build it from source, and links to
`docs/user-guide/` for the rest. How to use revier beyond that first use is
the guide's. The layer model, the manual driver, and cutting a release are
`docs/`, not README.
