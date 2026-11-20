# Reviewing

Call the Skill tool with `project-review:project-review-change` for the rules
review, or with `worktree-flow:worktree-review` to review a PR and push the
fixes. **Local delta:** the rules below, which win where they disagree with the
skill.

## Quality rules

How finished code must look. Each rule is a condition to check on the diff,
with the correct form.

- **Decision citation, form**: a Go comment cites a decision as
  `(decisions.md Dnn)`, several together as `(decisions.md Dnn, Dnn)`, and a
  later one in the same comment as `(Dnn)`. Flag a citation in any other form.
- **Decision citation, content**: a Go comment gives a number only to a fact
  that its entry or the design text tagged `(Dnn)` carries;
  [OVERVIEW.md](OVERVIEW.md#finding-things) has the search. Flag a number on
  any other fact: the comment states that fact in its own words, with no
  number.
- **Decision numbers, outside Go**: a commit message, a PR title or a PR
  body writes every decision number as `decisions.md Dnn`, several together
  as `decisions.md Dnn, Dnn`: a citation, and also a number that names the
  decision itself, as in "decisions.md D190 replaces decisions.md D21". A
  number placed next to a fact is a citation, and only for a fact that its
  entry or the design text tagged `(Dnn)` carries. A wrong form quoted in
  backticks, as in a report of it, is no number. Flag a number in any other
  form, or a citation on any other fact.
