# Statuses

A status shows the output of your command on the TUI, for example usage
limits or a CI result. revier runs the command on a timer, while a TUI is on
screen: the popup when it is shown, or `revier` in a terminal.

## Places

| `at` | Where | Runs for | Declared in |
|---|---|---|---|
| `rule-left` | Left end of the rule under the top line | The whole screen | `config.toml` only |
| `rule-right` | Right end of that rule | The whole screen | `config.toml` only |
| `project-line` | Right end of the pane's first line | The selected project | `config.toml`, project file, link file |
| `project` | A section of the pane, above the targets, with a title | The selected project | `config.toml`, project file, link file |

One status per place in a file. A second table at the same place is refused.

## Fields

```toml
# config.toml
[[status]]
at    = "project"
title = "CI"
run   = ["ci-status", "--short"]
every = "2m"
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `at` | string | required | The [place](#places). |
| `run` | list of strings | required, except in a project file that changes a shared table | The command, as an argv list. No shell, no templates. |
| `every` | duration | `60s` | Time between two runs, for example `"30s"`, `"2m"`, `"1h"`. At least `5s`. |
| `title` | string | `Info` | The section heading. Only at `project`. |

revier looks up a bare program name on `PATH`, expands a leading `~/`, and
reads any other relative path against your home directory. For shell syntax,
run a shell: `["sh", "-c", "gh pr list | wc -l"]`.

## Environment

The command runs in your home directory. At `project-line` and `project`,
revier adds:

| Variable | Value |
|---|---|
| `REVIER_PROJECT` | The project's name. |
| `REVIER_PROJECT_PATH` | The project's `path`. On a link, the path on the host. |
| `REVIER_PROJECT_HOST` | A link's host. Not set for a local project. |

To work in the checkout, start the command with `cd "$REVIER_PROJECT_PATH"`.

## Output

Print one of these on stdout. revier tries them in this order:

| Form | Example |
|---|---|
| JSON list of segments | `[{"text":"5h 92%","level":"critical"},{"text":"7d 41%"}]` |
| One JSON segment | `{"text":"CI failed","level":"critical"}` |
| Plain text | Each line that is not empty is a segment. |

`level` is `normal` (the default), `warning` (yellow) or `critical` (red,
bold). revier removes escape sequences from the text.

- On a line, segments that do not fit are left out: put the most important
  first. In the `project` section, each segment has its own line.
- Empty output with exit status 0 clears the place.
- A non-zero exit, or a run over 5 seconds, keeps the last answer, dimmed.
  The [log](../troubleshooting.md#the-log) has the reason.

## Per project

In a project file, a table at the place of a shared table changes only the
fields it sets. Leave out `run` to keep the shared command:

```toml
# projects/shop.toml
[[status]]
at    = "project"
title = "Pipeline"
every = "30s"
```

A project cannot turn off a shared status. A link file takes no status from
`config.toml`: each of its tables needs a `run`, and the command runs on this
machine.

## Example: Claude Code usage limits

This shows `5h 82%  ·  7d 41%` at the right end of the rule. Claude Code
gives its `statusLine` script a JSON object on stdin, with a `rate_limits`
field: `{"five_hour": {"used_percentage": 82.6, ...}, "seven_day": {...}}`.

1. Put this first in your `statusLine` script. It saves the field to a file.
   The rest of the script must read `$input`, not stdin, and still print your
   status line, for example `printf '%s' "$input" | jq -r '.model.display_name'`.

   ```sh
   input=$(cat)
   limits=$(printf '%s' "$input" | jq -c '.rate_limits // empty')
   if [ -n "$limits" ]; then printf '%s\n' "$limits" > "$HOME/.cache/claude-rate-limits.json"; fi
   ```

2. Write `~/bin/claude-limits` and run `chmod +x ~/bin/claude-limits`. It
   prints nothing when the file is older than 10 minutes, so old numbers do
   not show as current:

   ```sh
   #!/bin/sh
   file="$HOME/.cache/claude-rate-limits.json"
   [ -r "$file" ] || exit 1
   [ -n "$(find "$file" -mmin +10)" ] && exit 0

   jq -c '[
     {name: "5h", window: .five_hour},
     {name: "7d", window: .seven_day}
     | select(.window != null)
     | (.window.used_percentage | floor) as $used
     | {text: "\(.name) \($used)%",
        level: (if $used >= 90 then "critical"
                elif $used >= 75 then "warning"
                else "normal" end)}
   ]' "$file"
   ```

3. Declare it in `config.toml`:

   ```toml
   [[status]]
   at    = "rule-right"
   run   = ["~/bin/claude-limits"]
   every = "1m"
   ```

The numbers change only when a Claude Code session draws its status line.
