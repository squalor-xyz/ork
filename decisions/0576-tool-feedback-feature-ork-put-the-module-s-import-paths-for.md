---
{
  "id": 576,
  "title": "Tool feedback (feature, ork): put the module's import paths for the Charm libraries in the reviewer brief's probe-test r",
  "status": "decided",
  "key": "tool-feedback.ork-51410ea9",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: put the module's import paths for the Charm libraries in the reviewer brief's probe-test recipe\nRole: reviewer\nRun: ork-20261125-185255-fc37\nProject: ork\nItem: S556\nEvidence: my first probe imported github.com/charmbracelet/bubbletea and failed setup, but this module uses charm.land/bubbletea/v2",
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
  "rationale": "Owner chose 'File it' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S556"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
