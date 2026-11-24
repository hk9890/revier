# 0081: Projects and config are edited on revier's own screens, never in `$EDITOR`.

A screen lists every setting. It shows a shared target merged, marks each value
that is `config.toml`'s, and writes only the values that differ from it, so a
project never writes `config.toml`.

Rejected: `$EDITOR`, because it opens an empty file with no list of settings and
hides merged shared values.
