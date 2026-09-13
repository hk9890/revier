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

- Write an entry as the choice and the reason that decides it, in about five
  lines. Put the behaviour it produces in the design doc that owns it.
- Name a rejected alternative only where it would otherwise be proposed again.
- When a decision replaces another, delete the old entry, say "Replaces ..."
  in the new one, and repoint every `Dnn` citation in the tree. Git history
  keeps the old text.
- Never reuse a number.

## Deliberately absent

Do not create these until there is something to record. Each would cost a load
and return nothing:

| File | Create when |
|---|---|
| `docs/MONITORING.md` | revier writes a log or leaves evidence of a past run |
| `docs/REVIEWING.md` | this repository has a review rule the `code-review` skill cannot know |
| `CONTRIBUTING.md` | someone other than the author builds from source |

## README scope

`README.md` describes what revier is, how to install a release, and how to use
it. Building from source, the layer model, the manual driver, and cutting a
release are `docs/`, not README.
