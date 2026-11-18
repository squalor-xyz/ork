S206: ork pins and checks a project's adj contract (*proposed*). `ork projects
pin NAME [--relay N] [--adj M]` sets the slicer pin, the adj pin, or both
(slicer first). `ork projects unpin NAME` clears both when the project has
adj on; `--adj` clears only the adj pin. Doctor shows the adj pin, and a
run refuses a pin or a spoken version outside adj-json v1 before it starts
(adj 97). On a project with adj on, the slicer recovery says `unpin NAME`
clears both pins. Doctor puts the adj column on the next line (`adj-json v1
pinned` or `adj-json v1 unpinned`). Docs updated: `docs/design.md`,
`docs/boundaries.md`, `docs/guide.md`.
