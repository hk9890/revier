# Extending

Three levels, in the order most people need them. Interface signatures are in
[interfaces.md](interfaces.md).

## Level 1 — configuration

No code. Covers new projects, the targets bound to them, how each target is
realized, actions and statuses. The format, field by field, is the user
guide's [configuration reference](../user-guide/configuration.md), with
[statuses](../user-guide/extending/status.md) beside it. The binary embeds
those pages, and `revier assist --reference` prints them, so the format a
build prints is the one it reads.

What the format is built to keep true:

- A project's file name is its name (D80).
- `launch` runs only when `match` finds nothing. Every target is therefore a
  run-or-raise binding, not a launcher.
- `run` and `launch` are argv lists, rendered as Go `text/template` over
  `Project` by the core before a host sees them: there is no quoting to get
  wrong and no shell to inject into.
- Targets most projects share are declared once, in `config.toml`, in the
  same form as in a project file (D59). A table merges key by key; any other
  value replaces the shared one whole.
- A tab opens inside another target's instance and is found again by the
  mark revier gives it (D64). `panels` stand only on a tab (D128), and a tab
  needs a runtime with tabs (D70). `tabs` opens an instance with its tabs, and
  a listed tab that fails stops the ones after it (D126).
- A restore resumes the agents of a listed tab; a tab that runs a `launch`
  comes back without a resume (D68).
- A target carries a realization for each kind of project, because a local
  project and a link are reached with different tools (D82). A project file
  writes the part of its own kind.
- A link's `path` and `git_url` are the host's; a target here renders them,
  and neither names anything on this machine (D83).
- A link to the machine revier runs on is left out (D131).
- A status is a command whose output the surface draws at one place (D134).
  A link runs its own statuses alone: a shared command is written against a
  checkout on this machine.

Anything specific to one person — an editor, a ticket viewer, a browser, a
company tool — is a target, an action or a status. It never becomes code in
this repository.

## Level 2 — an external agent probe

For a coding agent revier does not know yet, without writing Go. The contract,
JSON in and out, is the user guide's
[agent probes](../user-guide/extending/probes.md) page.

One process runs per panel per refresh, so a probe stays cheap or it becomes
the reason the TUI feels slow; one that overruns its half-second timeout is
killed with everything it started. A compiled-in probe wins for the harness it
knows.

Of the ports, only `AgentProbe` has a subprocess form; a status is a command
too, but no port. A `Host` is stateful and sits in the latency path of every
keystroke, so it is Go or nothing.

## Level 3 — a Go host

For a new terminal, a new multiplexer, or a new compositor.

1. Implement `Host` in `internal/adapter/<name>/`: the methods
   [interfaces.md](interfaces.md#host) lists.
2. Implement `Capabilities` as well when the adapter is a `Runtime`, and return
   panels on the instances it lists. Only a runtime host can see inside a
   terminal, and the agent monitor reads nothing else. A panel's kind is
   `shell` where `revier.IsShell` says so and `tool` otherwise, never `agent`
   (D119).
3. Make `Probe(ctx)` return nil only when the adapter can genuinely work. It is
   how automatic selection stays correct on a machine that has several.
4. Add one line to `cmd/revier/adapters.go`.
5. Ship the host tests
   [TESTING.md](../TESTING.md#what-every-host-test-must-cover) requires.

What an adapter must never decide, and what `Instances` may cost, are in
[CODING.md](../CODING.md#adapters-hold-no-policy) and its
[two invariants](../CODING.md#two-invariants).

## What is not extensible, on purpose

The store is TOML files on disk. The output formats are the TUI and `--json`.
Neither is behind an interface, because neither has a second implementation
anyone has asked for. When one appears, the interface gets written then.
