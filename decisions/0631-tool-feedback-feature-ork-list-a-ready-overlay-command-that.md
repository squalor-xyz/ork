---
{
  "id": 631,
  "title": "Tool feedback (feature, ork): list a ready overlay command that prints each new render fixture's text so a reviewer can ",
  "status": "decided",
  "key": "tool-feedback.ork-ffbf8dc5",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: list a ready overlay command that prints each new render fixture's text so a reviewer can read the views without writing a probe test\nRole: reviewer\nRun: ork-20261127-160126-bfbb\nProject: ork\nItem: S633\nEvidence: I wrote TestProbeWaitViews in my temp dir only to t.Log renderViewText for wait-block, wait-lines and wait-stop",
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
    "S633"
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
