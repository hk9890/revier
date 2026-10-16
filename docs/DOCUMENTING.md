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

## `docs/user-guide/`

- Write for someone who installed revier and never opens this repository.
- Link only to pages inside `docs/user-guide/`. A user who reads the guide
  away from the repository finds every link working.
- Name a file path or a configuration key only where the user opens or sets
  it. A source path, a Go symbol, a `mise` task, a test layer, CI and release
  detail stay in `docs/`.
- Cite no decision. Where the user needs the reason, give it in one sentence;
  the argument stays in `docs/design/`.
- `configuration.md` and `extending/` are the configuration reference. The
  binary embeds them, and `revier assist --reference` prints them, so a fact
  about the format lives there and nowhere else.

## Conventions

- Cite a decision as `` `docs/design/decisions.md` Dnn `` from `docs/`, as
  `(Dnn)` inside `docs/design/`, and from a Go comment as
  [REVIEWING.md](REVIEWING.md#quality-rules) says.
- No check runs on Markdown: `mise run quality` and CI test only Go. Check
  links and anchors by hand.

## Deliberately absent

Do not create these until there is something to record. Each would cost a load
and return nothing:

| File | Create when |
|---|---|
| `CONTRIBUTING.md` | someone other than the author builds from source |

## README scope

`README.md` describes what revier is, how to install a release, a short first
use, and the two commands that build it from source, and links to
`docs/user-guide/` for the rest. How to use revier beyond that first use is
the guide's. The layer model, the manual driver, and cutting a release are
`docs/`, not README.
