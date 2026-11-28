---
{
  "id": 672,
  "title": "Tool feedback (feature, ork): when the brief's Base overlays note says an older binary made the list, also say whether t",
  "status": "decided",
  "key": "tool-feedback.ork-9f23ccbb",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: when the brief's Base overlays note says an older binary made the list, also say whether the slice's own per-test lines would have appeared\nRole: reviewer\nRun: ork-20261128-002828-9b009e14\nProject: ork\nItem: S643\nEvidence: brief Base overlays section carried the older-binary note and no per-test lines although baseoverlay_test.go changed",
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
  "decision": "drop",
  "rationale": "Owner chose 'Drop' in an interactive question, relay session 8d711f67, 2026-11-28: the older-binary note already warns the list may be stale.",
  "refs": [
    "S643"
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
