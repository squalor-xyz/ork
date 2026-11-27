---
{
  "id": 611,
  "title": "Tool feedback (feature, ork): in Base overlays, also list the cross-package test commands for packages that import the r",
  "status": "decided",
  "key": "tool-feedback.ork-5c310ab5",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-27",
  "context": "Proposal: in Base overlays, also list the cross-package test commands for packages that import the reverted file, such as ./internal/cli for engine/brief.go\nRole: reviewer\nRun: ork-20261126-021918-bdc3\nProject: ork\nItem: S607\nEvidence: the new test lives in internal/cli but the only overlay command listed was ./internal/engine; the same overlay-1.json worked for ./internal/cli",
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
  "rationale": "Owner chose 'File it' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S607"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
