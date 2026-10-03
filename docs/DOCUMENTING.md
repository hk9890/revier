# Documenting

The standard is the `instruction-writing:writing-project-docs` skill. Below is
this repository's delta, which wins where the two disagree.

## Two trees, two owners

| Tree | Owns | Register |
|---|---|---|
| `docs/*.md` | How to operate on this repository now | Command register: imperative, one instruction per line |
| `docs/design/` | What the system is intended to be, and why | Prose. A design doc argues; rationale is its content, not padding. |

`docs/design/README.md` states which design document owns what. Design docs are
not a record of what is built — the code is that record — so a design doc
describing something absent from `internal/` is intent, not documentation drift.

`docs/design/decisions.md` holds only decisions in force:

- Write an entry as the choice and the reason that decides it, in at most
  8 lines under its heading. Cut an entry that grows past 8 lines rather than
  extend the limit.
- Put the behaviour a decision produces in the design doc that owns it, tagged
  `(Dnn)`. Leave what describes an adapter or the core from the inside in the
  Go comment beside it: no design doc owns it.
- Name a rejected alternative only where it would otherwise be proposed again.
- When a decision replaces another, delete the old entry, say "Replaces ..."
  in the new one, and repoint every `Dnn` citation in the tree. Git history
  keeps the old text.
- Never reuse a number.

## Conventions

- Cite a decision as `` `docs/design/decisions.md` Dnn `` from `docs/`, as
  `(Dnn)` inside `docs/design/`, and from a Go comment as
  [CODING.md](CODING.md#citing-a-decision) says.
- No check runs on Markdown: `mise run quality` and CI test only Go. Check
  links and anchors by hand.

## Deliberately absent

Do not create these until there is something to record. Each would cost a load
and return nothing:

| File | Create when |
|---|---|
| `docs/REVIEWING.md` | this repository has a review rule the `code-review` skill cannot know |
| `CONTRIBUTING.md` | someone other than the author builds from source |

## README scope

`README.md` describes what revier is, how to install a release, how to use
it, and the two commands that build it from source. The layer model, the
manual driver, and cutting a release are `docs/`, not README.
