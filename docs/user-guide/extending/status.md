# Statuses

A status puts the output of a command of yours on the TUI: an account's usage
limits, a CI result, a count of open tickets. You write the command; revier
runs it on a timer and draws what it printed at the place you choose. This
page is the reference for the `[[status]]` table, the output a command
prints, and when it runs.

## The four places

| `at` | Where it stands | Runs for | Declared in |
|---|---|---|---|
| `rule-left` | The left end of the thin rule under the top line. Beside the pane, it stands over the list. | The whole screen, once | `config.toml` only |
| `rule-right` | The right end of the thin rule. Beside the pane, it stands over the pane. | The whole screen, once | `config.toml` only |
| `project-line` | The right end of the pane's first line, where the project's name leaves room. | The selected project | `config.toml`, a project file, a link file |
| `project` | A section of the pane, above the targets, under its own title. | The selected project | `config.toml`, a project file, a link file |

Each place shows one status. Two tables at the same place in one file are
refused: the first one stands.

## Declare a status

```toml
# config.toml
[[status]]
at    = "rule-right"
run   = ["usage-limits"]       # found on PATH
every = "2m"

[[status]]
at    = "project"
title = "CI"                   # the section's heading
run   = ["ci-status", "--short"]
```

| Key | Type | Default | Meaning |
|---|---|---|---|
| `at` | string | required | The place: `rule-left`, `rule-right`, `project-line` or `project`. |
| `run` | list of strings | required in `config.toml` | The command, as an argv list. |
| `every` | duration | `60s` | How long to wait between two runs. A Go duration such as `"30s"`, `"2m"` or `"1h"`. 5 seconds at least. |
| `title` | string | `Info` | The heading of the `project` section. Only at `project`. |

`run` is not a shell string and has no templates: the project reaches the
command through environment variables instead. The first word is the program:

- a bare name, such as `ci-status`, is looked up on `PATH`;
- a leading `~/` is your home directory;
- any other relative path is read against the directory the TUI runs in,
  which is your home directory.

To use shell syntax, run a shell: `["sh", "-c", "gh pr list | wc -l"]`.

## What the command is told

A status at `rule-left` or `rule-right` runs with revier's own environment.
A status at `project-line` or `project` runs for the selected project, with
these variables added:

| Variable | Value |
|---|---|
| `REVIER_PROJECT` | The project's name. |
| `REVIER_PROJECT_PATH` | The project's `path`. On a link, the path on the host. |
| `REVIER_PROJECT_HOST` | A link's host. Not set for a project on this machine. |

The command runs in your home directory, not in the project. Change into
`REVIER_PROJECT_PATH` where it needs the checkout:

```sh
#!/bin/sh
cd "$REVIER_PROJECT_PATH" || exit 1
git status --porcelain | wc -l | sed 's/$/ changed/'
```

For a link, `REVIER_PROJECT_PATH` is a path on another machine. Reach it
over ssh with `REVIER_PROJECT_HOST`.

## What the command prints

The command prints one of three forms on stdout. revier tries them in this
order:

1. A JSON list of segments:

   ```json
   [{"text":"5h 82%","level":"critical"},{"text":"7d 41%","level":"normal"}]
   ```

2. One JSON segment:

   ```json
   {"text":"CI failed","level":"critical"}
   ```

3. Plain text. Each line that is not empty is a segment, with no level.

A segment has `text` and an optional `level`. A segment with empty `text` is
dropped. revier removes escape sequences and control characters from the
text, and turns a tab into a space, so colour the command prints itself does
not reach the screen. Use `level` instead.

Output that is empty, with exit status 0, clears the place.

## Levels and their colours

| `level` | Colour |
|---|---|
| `normal`, or none | The theme's quiet text colour. |
| `warning` | The theme's warning colour: yellow. |
| `critical` | The theme's attention colour: red, bold. |

An unknown level is drawn as `normal`. The colours are the theme's `[ui]
theme`; you cannot set them per status.

## How a segment fits its line

- **On a line** (`rule-left`, `rule-right`, `project-line`), the segments are
  joined with ` · ` between them. Where the line has no room for the next
  whole segment, that segment and every one after it are left out. revier
  never cuts a segment in half, because half a value reads as another value.
  Put the segment that matters most first.
- **On the rule**, a status stands two columns in from the edge. A status
  with no room at all is left off, and the rule runs through.
- **In the `project` section**, each segment has a line of its own. A
  segment wider than the pane ends in an ellipsis.

The `project` section appears only once its command has printed something.

## When it runs

- revier looks for a status that is due every 5 seconds. A status is due when
  its `every` has passed since its last run. So it runs at most 5 seconds
  late, and `every` is never less than 5 seconds.
- A status with no `every` runs once a minute.
- A run that has not ended after 5 seconds is ended: revier kills the command
  and every process it started.
- Each place runs one command at a time. When you move the cursor to another
  project, its pane statuses run at once, or as soon as the run still out at
  that place ends. Moving the cursor down the list costs one project's runs,
  not one per project.
- Nothing runs while the popup is hidden. When you raise it, what fell due
  meanwhile runs. A TUI without the focus keeps running its statuses.
- A status you change in a file runs at once on the next reload, and its old
  answer is no longer shown.

A run that exits with a status other than 0, or that is ended at 5 seconds,
keeps the last answer on the screen, dimmed. A place that never had an
answer stays empty. The log names why the run failed, with what the command
wrote on stderr. See [troubleshooting.md](../troubleshooting.md).

## Statuses per project

A project file and a link file declare `project-line` and `project` only.

- **A table at the place of a shared one** changes only the fields it sets,
  as a target's override does. Leave out `run` to keep the shared command;
  set `title` or `every` to change only that.
- **A table at a place no shared table fills** is the project's own, and
  needs a `run`.
- A project cannot switch a shared status off.

```toml
# projects/shop.toml
path = "~/dev/shop"

# Keep the shared CI command, under another heading and more often.
[[status]]
at    = "project"
title = "Pipeline"
every = "30s"
```

A link takes no status from `config.toml`. A shared command is written
against a checkout on this machine, and a link's checkout is on its host. A
link runs its own tables alone, so each of them needs a `run`. The command
runs here, with `REVIER_PROJECT_HOST` set. The host's project file is not
read.

A project with no `path`, such as one whose file did not load, takes no
status from `config.toml`: the command would read your home directory as the
checkout.

## What is refused

`revier doctor` names each refused table. A refused table costs only itself.

- `at` missing, or not one of the four places;
- `rule-left` or `rule-right` in a project or link file;
- `title` at a place other than `project`;
- no `run` in `config.toml`, or no `run` in a project's table with no shared
  table at its place to change;
- no `run` in a link file's table;
- `every` that is not a duration, or is under 5 seconds;
- a second table at a place the same file already has.

## Example: Claude Code usage limits on the rule

This puts your Claude Code 5-hour and 7-day usage on the right end of the
rule, red when a window is nearly used up:

```
──  5h 82%  ·  7d 41%  ──
```

Claude Code hands its status line command a JSON object on stdin, and its
`rate_limits` field holds `five_hour` and `seven_day`, each with a
`used_percentage`. A revier status cannot read that stdin, so the status line
command saves the field to a file, and the status reads the file.

1. In the script your Claude Code `statusLine` setting runs, save the field.
   Read stdin once, into a variable, and use the variable for the rest of the
   script:

   ```sh
   input=$(cat)
   limits=$(printf '%s' "$input" | jq -c '.rate_limits // empty')
   [ -n "$limits" ] && printf '%s\n' "$limits" > "$HOME/.cache/claude-rate-limits.json"
   ```

2. Write the status command, `~/bin/claude-limits`, and make it executable
   with `chmod +x ~/bin/claude-limits`:

   ```sh
   #!/bin/sh
   # Prints Claude Code's usage limits as revier status segments.
   file="$HOME/.cache/claude-rate-limits.json"
   [ -r "$file" ] || exit 1

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

The file is as fresh as the last Claude Code session that drew its status
line. Until one has, the script exits 1 and the place stays empty.

## Quick reference

| | |
|---|---|
| Places | `rule-left`, `rule-right` (`config.toml` only); `project-line`, `project` |
| Fields | `at`, `run`, `every` (default `60s`, at least `5s`), `title` (`project` only, default `Info`) |
| Environment | `REVIER_PROJECT`, `REVIER_PROJECT_PATH`, `REVIER_PROJECT_HOST` (pane places only) |
| Output | JSON list of `{text, level}`, one such object, or plain lines |
| Levels | `normal`, `warning`, `critical` |
| Timing | checked every 5 s; ended after 5 s; nothing runs while the popup is hidden |
| Failure | the last answer stays, dimmed; the log says why |
