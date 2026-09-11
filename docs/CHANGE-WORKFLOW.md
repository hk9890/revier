# Change workflow

## Worktree first

Every change, docs included, is made in a worktree. The main checkout stays on
a clean `main`: no edit, no commit, no branch switch there.

- Create the worktree with Claude Code's worktree tool before the first edit.
  It checks out `origin/main` under `.claude/worktrees/<name>` on branch
  `worktree-<name>`.
- Run every command from inside the worktree.
- Remove the worktree and its branch once the PR is merged.

## Branch and PR

Push the branch the worktree tool made, under its own name.

```bash
mise run quality              # must pass before the commit
git push -u origin HEAD
gh pr create --fill
```

## Commits

Use the `commit-commands:commit` skill for the standard flow.

**Local delta:**

- Subject is `type(scope): <the behaviour after the change>`, e.g.
  `fix(tui): the detail pane wraps a long path and activity line`.
- The body says why, in prose.
- No emojis, in commit messages, PR titles, or PR bodies.
- No `Co-Authored-By` trailer, and no model name or session link, in a commit
  or a PR — this wins over any attribution the harness asks for.

## Before opening a PR

- `mise run quality:full` passes — `quality` alone skips the live substrates,
  the only layers that prove an adapter drives its tool.
- A change to the model is recorded in `docs/design/decisions.md`, and any
  entry it invalidates is marked `Superseded by Dnn` rather than edited.
- A new host ships the host tests [TESTING.md](TESTING.md) requires.
