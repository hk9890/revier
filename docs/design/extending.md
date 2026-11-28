# Extending

Three levels, in the order most people need them.

## Level 1 — configuration

No code. New projects, their targets, actions and statuses are configuration;
the format is the user guide's [configuration reference](../user-guide/configuration.md).

The format is built to keep three things true:

- Every target is a run-or-raise binding, never a launcher, as the
  [model](product.md#model) says.
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
feels slow.

Of the ports, only the agent probe has a subprocess form. A runtime, a view
and a window host are stateful and sit in the latency path of every
keystroke, so each is Go or nothing.

## Level 3 — a Go runtime, view or window host

A new terminal runtime, terminal view or compositor is a Go adapter behind
one of the three ports that run and show programs
([architecture.md](architecture.md#ports)): a runtime lists and starts
terminals, a view shows and focuses them, and a window host lists, opens and
raises OS windows. Each holds no policy, and reports whether it can work where
it runs, which keeps automatic selection correct on a machine that has
several. What an adapter must never decide is in
[CODING.md](../CODING.md#adapters-hold-no-policy).

## What is not extensible, on purpose

The store and the output formats are not behind an interface:
[architecture.md](architecture.md#the-other-ports) says why.
