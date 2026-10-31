# Design

Forward-looking design for revier: the model it is intended to have, its
boundaries, and why. How this tree relates to `docs/` is [../DOCUMENTING.md](../DOCUMENTING.md)'s.

| Document | Owns |
|---|---|
| [product.md](product.md) | What revier is, the job it does, and what it deliberately leaves out |
| [architecture.md](architecture.md) | The port-and-adapter structure, why `pkg/revier` is public, and adapter selection |
| [extending.md](extending.md) | The three levels at which someone extends revier |
| [decisions.md](decisions.md) | The decision log: what was chosen, when, and why |

Each fact lives in exactly one of these. A document that needs a fact another
one owns links to it.

[CODING.md](../CODING.md#where-a-change-goes) says when to search
decisions.md before a change.
