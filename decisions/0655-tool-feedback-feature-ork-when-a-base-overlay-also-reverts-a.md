---
{
  "id": 655,
  "title": "Tool feedback (feature, ork): when a base overlay also reverts a test file, also offer an overlay that reverts only the ",
  "status": "decided",
  "key": "tool-feedback.ork-3f3b079c",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: when a base overlay also reverts a test file, also offer an overlay that reverts only the product file plus a stub, so the new tests can show a red result without a hand-written probe\nRole: reviewer\nRun: ork-20261127-194832-e5c3\nProject: ork\nItem: S629\nEvidence: overlay-2 reverted brief_test.go along with round.go, so TestReviewerReceivesRoundCheckEvidence could not run, and a probe test was needed to show it goes red",
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
  "rationale": "Folded into S643: Jon chose 'Fold into S643' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S629"
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
