# Design

Forward-looking specification for revier: what the system is intended to be, and
why. How this tree relates to `docs/` is [../DOCUMENTING.md](../DOCUMENTING.md)'s.

| Document | Owns |
|---|---|
| [product.md](product.md) | What revier is, the job it does, and what it deliberately leaves out |
| [architecture.md](architecture.md) | The port-and-adapter structure, package layout, and adapter selection |
| [interfaces.md](interfaces.md) | The Go type and interface definitions the ports are made of |
| [extending.md](extending.md) | The three levels at which someone extends revier |
| [performance.md](performance.md) | What the hot path costs, measured, and what follows |
| [decisions.md](decisions.md) | The decision log: what was chosen, when, and why |

Each fact lives in exactly one of these. A document that needs a fact another
one owns links to it.
