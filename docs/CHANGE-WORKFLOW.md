# Change workflow

## Branch and PR

`main` is not developed on. Every change is a branch and a PR.

```bash
git switch -c <topic>
mise run quality              # must pass before the commit
git push -u origin <topic>
gh pr create --fill
```

Run `mise run quality:full` before handing work back, not `quality`: it adds the
live substrates, which are the only layers that prove an adapter drives its tool.

## Commits

No emojis, in commit messages, PR titles, or PR bodies.

No `Co-Authored-By` trailer, and no model name or session link, in a commit or
a PR.

## Before opening a PR

- `mise run quality:full` passes.
- A change to the model is recorded in `docs/design/decisions.md`, and any
  entry it invalidates is marked `Superseded by Dnn` rather than edited.
- A new host ships the two invariant tests named in [CODING.md](CODING.md).
