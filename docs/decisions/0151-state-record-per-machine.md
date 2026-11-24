# 0151: The state record is kept per machine, apart from usage events.

A failure or a closed window is no use of a project, so usage events cannot hold
it. An operation records what it did, failed or not, and a surface records what
it saw as the difference between two surveys. A host that did not answer is
unknown, never empty.

Rejected: merging a linked host's record, because it shows that host's agents
twice.
