---
{
  "id": 619,
  "title": "Tool feedback (feature, ork): add an ork wait fixture to -render-fixture so the UI pass does not need an overlay test fo",
  "status": "decided",
  "key": "tool-feedback.ork-b6eb50ca",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: add an ork wait fixture to -render-fixture so the UI pass does not need an overlay test for wait output\nRole: ui\nRun: ork-20261127-131403-3844\nProject: ork\nItem: S621\nEvidence: the -render-fixture list in the brief has no wait view; I wrote TestRenderWaitOverlay to render it",
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
    "S621"
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
