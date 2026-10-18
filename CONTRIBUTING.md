# Contributing to revier

This file is the entry point for human contributors. It points to the doc that
owns each step. AI agents start from [AGENTS.md](AGENTS.md).

## Getting started

1. Build from source: see [Building](README.md#building).
2. Learn where code goes and what a host must never do: see
   [docs/CODING.md](docs/CODING.md).
3. Run the tests and learn the layers: see [docs/TESTING.md](docs/TESTING.md).
4. Run revier by hand without touching your own desktop: see
   [docs/RUNNING.md](docs/RUNNING.md).

## Before you open a PR

- `mise run quality:full` passes.
- The commit subject and body follow
  [docs/CHANGE-WORKFLOW.md](docs/CHANGE-WORKFLOW.md#commits).
- The change meets the rules in [docs/REVIEWING.md](docs/REVIEWING.md).
- A change to the model is recorded in `docs/design/decisions.md`; see
  [docs/design/README.md](docs/design/README.md).

## Where things live

- Using revier: [docs/user-guide/](docs/user-guide/README.md)
- Finding your way around the source: [docs/OVERVIEW.md](docs/OVERVIEW.md)
- Cutting a release: [docs/RELEASING.md](docs/RELEASING.md)

Open an issue before a large change, so we can agree on the approach first.
