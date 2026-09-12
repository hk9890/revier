# Releasing

`.github/workflows/release.yml` publishes releases with GoReleaser, dispatched
by hand against a release tag. The run builds, vets and tests before
publishing, so a green run is the provenance for that commit.

A release carries 2 archives (linux x64 and arm64, each holding `revier`,
`revier-popup` and `revier-go`), a per-archive SPDX SBOM (syft), a checksums
file signed keyless with cosign (`.sig` + `.pem`), and SLSA build provenance
from `actions/attest-build-provenance`. Archive names —
`revier_0.1.0_linux_x64.tar.gz` — let installers such as `mise` auto-detect
the right asset. Version injection: `internal/build`, stamped by
`.goreleaser.yaml` in a release and by `mise run build` locally.

Two settings that look wrong and are not: cosign is pinned to **v2.6.3**
because v3 defaults to `--new-bundle-format`, which the `--output-signature`
and `--output-certificate` flags in `.goreleaser.yaml` do not accept; and the
provenance step keeps `continue-on-error` because attestation is unavailable
on a user-owned **private** repository.

## Cut a release

`mise run quality:full` gates every step. Drive the binary per
[RUNNING.md](RUNNING.md) as well when the release changes runtime behaviour.

1. Pick `vX.Y.Z` and confirm it is free: `git tag --list "v*"`.
2. Write the `CHANGELOG.md` section (see below) and land it like any other
   change: worktree, PR, merge ([CHANGE-WORKFLOW.md](CHANGE-WORKFLOW.md)).
3. Fast-forward `main` to the merged commit and run `mise run quality:full`
   on it.
4. Tag that commit and push the tag:

   ```bash
   git tag -a vX.Y.Z -m "revier vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. Dispatch the workflow and watch it. The run takes a few seconds to
   register; re-run the lookup if it returns nothing. GoReleaser creates or
   updates the GitHub release itself. Its notes are the tag's `CHANGELOG.md`
   section, printed by `scripts/release-notes vX.Y.Z`; a tag with no section
   fails the run before it publishes.

   ```bash
   gh workflow run release.yml --ref vX.Y.Z
   gh run watch "$(gh run list --workflow=release.yml --branch vX.Y.Z --limit 1 --json databaseId -q '.[0].databaseId')" --exit-status
   ```

6. Verify. Expect 2 archives, 2 `.sbom.json`, `…_checksums.txt` and both
   `…_checksums.txt.sig` and `…_checksums.txt.pem`, and a body that is the
   tag's CHANGELOG section — an empty body means GoReleaser dropped the notes
   file, and `gh release edit vX.Y.Z --notes-file` repairs it without a
   re-dispatch.

   ```bash
   gh release view vX.Y.Z --json assets,body -q '.body, (.assets[].name)'
   gh release download vX.Y.Z -p '*_linux_x64.tar.gz'
   gh attestation verify revier_X.Y.Z_linux_x64.tar.gz -R hk9890/revier
   ```

**The dispatched ref must be the tagged commit.** `workflow_dispatch` reads
the workflow file from the ref it runs on, and GoReleaser requires the
checked-out commit to be the tagged one. A `release.yml` change that a release
needs must live in the tagged commit — commit it, then move the tag
(`git tag -f -a vX.Y.Z -m … && git push -f origin vX.Y.Z`).

Re-dispatching against an existing tag overwrites its assets instead of
failing (`release.replace_existing_artifacts`). To add a missing asset without
a new tag: `gh release upload vX.Y.Z dist/* --clobber`.

## What goes in the CHANGELOG section

The section is the release text on GitHub. Write it for the operator, not for
this repository: its reader runs `revier` and has never opened the source.
Features, changed behaviour and fixes, never a commit list. An entry earns its place by telling them what
they must do, what they will see that they did not see before, or what was
wrong that is now fixed. State the symptom; the cause belongs in the commit
message.

- Never name an internal symbol, a Go package, a third-party library, or a
  test. `Instances issued one list-panes per project` is a commit message;
  `the TUI stalled for a second on every refresh` is an entry.
- A change with no user-visible effect gets no entry — a refactor, added
  coverage, a doc fix, a dependency bump nobody has to act on.
- Anything that now refuses a project file, a config or a habit the operator
  already has leads with **Action required** and names the fix.

## Local fallback

When Actions can't be used. Needs `goreleaser` v2 in PATH
(`go install github.com/goreleaser/goreleaser/v2@latest`), a clean tree on
the tagged commit, and `mise run quality:full` green there — that run
substitutes for the CI provenance.

```bash
scripts/release-notes vX.Y.Z > /tmp/release-notes.md
GITHUB_TOKEN=$(gh auth token) goreleaser release --clean --skip=sign --skip=sbom --release-notes /tmp/release-notes.md
```

This produces binaries and checksums but no signing, SBOMs or provenance. Drop
`--skip=sign` with `cosign` installed — keyless signing locally needs an
interactive Sigstore flow, which the workflow avoids with its OIDC token — and
drop `--skip=sbom` with `syft` installed. SLSA provenance needs that same OIDC
token and cannot be produced locally. Verify as in step 6.

## Verifying a downloaded release

The signing identity is the release workflow file at the ref the release was
dispatched on — a tag ref for the flow above.

```bash
cosign verify-blob \
  --certificate revier_<version>_checksums.txt.pem \
  --signature  revier_<version>_checksums.txt.sig \
  --certificate-identity "https://github.com/hk9890/revier/.github/workflows/release.yml@refs/tags/v<version>" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  revier_<version>_checksums.txt
sha256sum -c revier_<version>_checksums.txt --ignore-missing
```
