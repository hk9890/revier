# Documenting

The standard is the `instruction-writing:writing-project-docs` skill. Below is
this repository's delta, which wins where the two disagree.

## Three trees, three owners

| Tree | Owns | Register |
|---|---|---|
| `docs/*.md` | How to operate on this repository now | Command register: imperative, one instruction per line |
| `docs/design/` | What the system is intended to be, and why | Prose. A design doc argues; rationale is its content, not padding. |
| `docs/user-guide/` | How to use revier as it is built: every command, file, field and key a user meets | Task prose, second person: "to do X, do Y". |

`docs/design/README.md` states which design document owns what. Design docs are
not a record of what is built — the code is that record — so a design doc
describing something absent from `internal/` is intent, not documentation drift.

A design doc holds the model, its boundaries, scope and non-goals, and the
reasons. Keys, flags, numbers, field names, widths and Go code belong to the
user guide or the code; write them there, not in `product.md`,
`architecture.md` or `extending.md`.

`docs/design/decisions.md` holds only decisions in force:

- Keep the log one file. Two branches that take the same number then meet as
  a merge conflict; one file per decision would merge both without a word,
  and its entries would be a heading and a few lines each. Do not move the
  log to a folder of ADR files.
- Write an entry as the choice and the reason that decides it, in at most
  8 lines under its heading. Cut an entry that grows past 8 lines rather than
  extend the limit.
- Leave what describes an adapter or the core from the inside in the Go
  comment beside it: no design doc owns it.
- Name a rejected alternative only where it would otherwise be proposed again.
- When a decision replaces another, delete the old entry, say "Replaces ..."
  in the new one, and repoint every `Dnn` citation in the tree. Git history
  keeps the old text.
- When an entry is withdrawn and nothing replaces it, delete it and remove
  every `Dnn` citation of it in the tree. A Go comment that cited it keeps its
  own words, with no number.
- Every `Dnn` cited in the tree names a `### Dnn` heading of
  `decisions.md`, and no two headings share a number. A "Replaces Dnn" is no
  citation. After a rebase onto `main`, check the number of a new entry
  against `main`'s headings.
- Never reuse a number.

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
  the argument stays in `docs/design/`.
- `cli.md` names every command with a one-line purpose. Flag detail stays with
  `--help`, except a flag a task page needs.
- `configuration.md` and `extending/` are the configuration reference. The
  binary embeds them, and `revier assist --reference` prints them, so a rule
  or field of the format is written there and nowhere else. Another page
  shows at most an example and links to the reference section.

## Conventions

- Cite a decision as `` `docs/design/decisions.md` Dnn `` from `docs/` outside
  `docs/design/`, and from a Go comment as [REVIEWING.md](REVIEWING.md#quality-rules) says.
- Inside `docs/design/`, tag the concept a decision sets `(Dnn)`. Write
  a bare `Dnn` where the prose names the decision itself, as in "Replaces
  D71".
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
