# 0082: A target carries a local and a remote realization, so one name and key serve both.

The editor of a project on another machine can be VS Code over ssh where the
local one is IntelliJ on a path here. One realization cannot hold both, so a
link reads the remote part.

Rejected: two targets, because they split the name and the key that must stay
one.
