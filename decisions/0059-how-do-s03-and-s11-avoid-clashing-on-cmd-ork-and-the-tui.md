---
{
  "id": 59,
  "title": "How do S03 and S11 avoid clashing on cmd/ork and the TUI?",
  "status": "decided",
  "key": "plan.s03-wiring",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "S11 creates cmd/ork/main.go and may change internal/source; S03 needs the same wiring.",
  "options": [
    {
      "label": "s03-plus-s13",
      "summary": "S03 packages only, S13 wires the binary"
    },
    {
      "label": "wait",
      "summary": "S03 runs after S11 with the wiring"
    }
  ],
  "decision": "s03-plus-s13",
  "rationale": "Owner 2026-10-19: S03 builds internal/daemon, internal/ipc and internal/cli without touching cmd/ork or the TUI; a size-S item S13 wires the binary and moves the TUI onto the socket client after both land.",
  "refs": [
    "S03"
  ],
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
