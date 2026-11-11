---
{
  "id": 323,
  "title": "How is the new global `owner` and `file-answers` setting stored, and does the registry version change?",
  "status": "decided",
  "key": "owner.setting-store",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: How is the new global `owner` and `file-answers` setting stored, and does the registry version change?\nOptions:\n1. registry v3: add `owner` and `file_answers` to the registry file and bump `registry.Version` to 3. v1 and v2 files still load, and an older ork refuses a v3 file with a clear version error. This is how `daemon_idle_minutes` was added in v2 (`internal/registry/registry.go:20-23,286-293`, `docs/state.md:85`).\n2. registry v2: add the omitempty keys with no bump. A file with the keys unset is unchanged. Once either is set, an older ork's strict decode fails with an unknown-key error instead of a version error.\n3. separate file: a small file in the ork state directory, leaving `projects.json` and its version alone. An older ork is unaffected, but there is a second settings file for the owner to know about.\nRecommend: 1 - it follows the precedent for file-level keys and gives a rollback error that names the version.\nDecision key: owner.setting-store\n\nNote: no question needed for the Check list. The `internal/tui` and `ork doctor` lines are the UI pass, a separate relay row under rule u10, not commands. Every other Check command (`go test ./...`, `go vet ./...`, `gofmt -l .`, `slicer check`, `adj lint`) is runnable with a clear result. Three anchors have drifted:\n- `internal/tui/keys.go:113` is now the `FileAdj` binding at `keys.go:139`.\n- `docs/design.md:1720` is now the permissions paragraph, not adj filing.\n- Spec-check answers today are filed whenever the project has `adj` set (`internal/tui/speccheck.go:253`, `internal/daemon/speccheck.go:412`). The ctrl+a toggle governs run-pane answers (`internal/tui/pane.go:363`). The implementer can resolve both by reading.",
  "decision": "registry v3: add `owner` and `file_answers` to the registry file and bump `registry.Version` to 3. v1 and v2 files still load, and an older ork refuses a v3 file with a clear version error. This is how `daemon_idle_minutes` was added in v2 (`internal/registry/registry.go:20-23,286-293`, `docs/state.md:85`).",
  "rationale": "Answered in ork on 2026-11-11: How is the new global `owner` and `file-answers` setting stored, and does the registry version change?",
  "refs": [
    "S314"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
