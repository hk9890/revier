# Extending

Three levels, in the order most people need them.

## Level 1 — configuration

No code. New projects, their targets, actions and statuses are configuration;
the format is the user guide's [configuration reference](../user-guide/configuration.md).

The format is built to keep three things true:

- Every target is a run-or-raise binding, never a launcher: revier starts a
  program only when it finds nothing to raise.
- Every command is an argument list, never a shell string, so there is no
  quoting to get wrong and no shell to inject into.
- Anything one person needs — an editor, a ticket viewer, a browser, a
  company tool — is a target, an action or a status. It never becomes code
  in this repository.

## Level 2 — an external agent probe

An external probe lets a user add a coding agent revier does not know yet,
without writing Go. The contract is the user guide's
[agent probes](../user-guide/extending/probes.md) page. A probe runs for every
panel on every refresh, so it stays cheap or it becomes the reason the surface
feels slow. It wins over every compiled-in probe except the Claude probe.

Of the ports, only the agent probe has a subprocess form. A host is stateful and
sits in the latency path of every keystroke, so it is Go or nothing.

## Level 3 — a Go host

A new terminal, multiplexer or compositor is a Go adapter behind the host
port ([architecture.md](architecture.md#four-ports)). It lists, opens and
focuses, holds no policy, and reports whether it can work where it runs, which
keeps automatic selection correct on a machine that has several. What a host
must never decide is in [CODING.md](../CODING.md#adapters-hold-no-policy).

## What is not extensible, on purpose

The store is TOML files on disk. The output formats are the TUI and JSON.
Neither is behind an interface, because neither has a second implementation
anyone has asked for. When one appears, the interface gets written then.
