# AGENTS.md — revier routing

## Repository purpose

revier is a project-grouped control surface for running coding agents: it shows
what every agent across every project is doing and reaches the one that needs
you. Every operation is the same one — run-or-raise a named target, and
remember where you came from. Go, ports-and-adapters, with the policy in
`internal/core` and every tool-specific fact behind a port in `pkg/revier`.

## Use-case routing

Every route below is **mandatory, not advisory**. Load the document BEFORE the
first action of that kind — loading it afterwards does not count, and no route
becomes skippable because the change looks small.

### Any change, commit, branch, PR

**MUST read [docs/CHANGE-WORKFLOW.md](docs/CHANGE-WORKFLOW.md) before the first
edit to ANY file in this repository, and before any git command that writes.**
No change is made in the main checkout; the doc says where it is made instead.

### Research, planning, architecture — and finding anything at all

**MUST read [docs/OVERVIEW.md](docs/OVERVIEW.md) before your first `rg`, `grep`,
`ls` or `Glob` of the source tree, and before writing any plan.** Which package
owns a decision is not guessable from the tree.

### Coding and file changes

**MUST read [docs/CODING.md](docs/CODING.md) before creating or editing ANY file
under `cmd/`, `pkg/`, `internal/`, or `scripts/`.** Two invariants there are not
visible from the code — what a host must never do, and what `Open` owes `Match`.

### Design intent, and any change to the model

**MUST read [docs/design/README.md](docs/design/README.md) before adding a port,
a type that crosses the port boundary, or a concept to the model.** The design
set carries the decision log, including which earlier models were superseded and
why, so a rejected idea is not re-proposed as new.

### Testing and verification

**MUST read [docs/TESTING.md](docs/TESTING.md) before writing a test, before
your first `go test` or `mise run test*`, and before reporting a change as
green.** The layer a test belongs in decides whether it can run at all on a
machine with no display.

### Running revier by hand

**MUST read [docs/RUNNING.md](docs/RUNNING.md) before running anything that
opens a window, starts a terminal, or talks to a window manager.** This product
manipulates the user's live desktop; the doc carries the substrates that let you
verify a change without touching it.

### Finding out what revier did

**MUST read [docs/MONITORING.md](docs/MONITORING.md) before you open a log file
or explain a past run, restore or keypress.** Every process writes to one daily
file, so a line means nothing until you know which process wrote it.

### Cutting a release, or changing what a release ships

**MUST read [docs/RELEASING.md](docs/RELEASING.md) before tagging a version,
before editing `.github/workflows/release.yml` or `.goreleaser.yaml`, and
before adding, renaming or removing a released artifact.** The workflow is
dispatched by hand, and the dispatched ref must be the tagged commit.

### Writing project docs

**MUST read [docs/DOCUMENTING.md](docs/DOCUMENTING.md) AND invoke the
`instruction-writing:writing-project-docs` skill before creating or editing ANY
Markdown file** — the root steering files, anything under `docs/`, or anything
under `docs/design/`. The two trees have different owners and different
registers, and some gaps in the set are deliberate.
