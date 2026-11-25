---
{
  "id": 560,
  "title": "Tool feedback (feature, ork): put the go test -overlay probe recipe in the reviewer brief so a reviewer can add a probe ",
  "status": "decided",
  "key": "tool-feedback.ork-89105671",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: put the go test -overlay probe recipe in the reviewer brief so a reviewer can add a probe test without writing to the tree\nRole: reviewer\nRun: ork-20261125-145554-ed21\nProject: ork\nItem: S543\nEvidence: I built overlay.json by hand to add a package-internal probe test for the in-place edit case",
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
  "rationale": "Owner chose 'Fold into S554' in an interactive question, relay session 8d711f67, 2026-11-25: S554 adds the go test -overlay probe recipe to the reviewer brief, not its own slice.",
  "refs": [
    "S543"
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
