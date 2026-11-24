# 0059: A shared target merges tables key by key and replaces every other value whole.

Most projects repeat the same targets, so `config.toml` declares them once. A
project overrides a table key by key, and any other value, a list included,
replaces the shared one.

Rejected: merging lists, because a panel has no identity to merge on.
