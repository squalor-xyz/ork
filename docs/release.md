# Release

Moved from AGENTS.md. Same text, not a new decision.

*Proposed:* the owner writes a `## vX.Y.Z (YYYY-MM-DD)` section in
`CHANGELOG.md`, creates an annotated `vX.Y.Z` tag whose message is the short
release summary, and pushes that tag. Check the Release Actions run after
the push. ork does not create or push tags.

The proposed workflow tests on Ubuntu and macOS before building darwin/arm64,
darwin/amd64 and linux/amd64 archives. Each archive holds `ork`, `LICENSE`,
`NOTICE` and `README.md`. It uploads archives and `SHA256SUMS` to the release.
New release notes use the annotated tag message followed by the matching
CHANGELOG section; lightweight tags use the section alone. A new release
fails before it is created when `CHANGELOG.md` has no section for the tag.
A signed tag's signature block is left out of the notes. An existing
release keeps its notes and receives replacement assets with `--clobber`.

After landing, the owner first runs `workflow_dispatch` from the Actions tab.
This proposed dry run tests and builds, retains the archives and checksums
as workflow artifacts, and never creates or edits a release. Its version is
`dev-` plus the short commit SHA. Confirm both test platforms pass, all three
archives and checksums exist, and the binaries print that version with
`--version` before pushing a release tag.
