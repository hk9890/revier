# Reviewing

Call the Skill tool with `project-review:project-review-change` for the rules
review, or with `worktree-flow:worktree-review` to review a PR and push the
fixes. **Local delta:** the rules below, which win where they disagree with the
skill.

## Quality rules

How finished code must look. Each rule is a condition to check on the diff,
with the correct form.

- **Decision citation, form**: a Go comment cites a record as
  `(decisions/NNNN)`, several together as `(decisions/NNNN, decisions/NNNN)`.
  Flag a citation in any other form.
- **Decision citation, content**: a Go comment gives a number only to a fact
  that its record carries; [OVERVIEW.md](OVERVIEW.md#finding-things) has the
  search. Flag a number on any other fact: the comment states that fact in its
  own words, with no number.
- **Decision numbers, outside Go**: a commit message, a PR title or a PR body
  writes every record number as `decisions/NNNN`. A number placed next to a
  fact is a citation, and only for a fact that its record carries. A wrong form
  quoted in backticks, as in a report of it, is no number. Flag a number in any
  other form, or a citation on any other fact.
- **Call-site comment**: a new, renamed or changed log `msg` (its fields, or
  when it is written) has a comment at its call site that says when it is
  written and which fields it carries. A new or changed state record kind has
  the same in the comment of its `Kind` constant in `internal/record`.
  [MONITORING.md](MONITORING.md) reads a line's meaning from that comment
  alone. Flag a `msg` or a kind without one, or a comment the change made
  false.
