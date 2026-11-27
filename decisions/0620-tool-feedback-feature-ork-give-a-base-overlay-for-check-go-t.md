---
{
  "id": 620,
  "title": "Tool feedback (feature, ork): give a base overlay for check.go that also reverts brief.go when brief.go reads the new fi",
  "status": "decided",
  "key": "tool-feedback.ork-6ff8d4a4",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: give a base overlay for check.go that also reverts brief.go when brief.go reads the new field, so the check.go revert check builds\nRole: reviewer\nRun: ork-20261127-131405-8913\nProject: ork\nItem: S615\nEvidence: Base overlays said check.go does not build: brief.go:820:12 w.quoted undefined",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "decision": "file",
  "rationale": "Owner chose 'File both' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S615"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
