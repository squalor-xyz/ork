---
{
  "id": 559,
  "title": "Tool feedback (feature, ork): give the reviewer a ready overlay command that runs the new tests against the base states.",
  "status": "decided",
  "key": "tool-feedback.ork-1c6a30fa",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: give the reviewer a ready overlay command that runs the new tests against the base states.go\nRole: reviewer\nRun: ork-20261125-144923-e7b7\nProject: ork\nItem: S545\nEvidence: reviewer could not run the revert mutations under read-only rules and relied on the implementer's table (same as S554)",
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
  "rationale": "Owner chose 'Drop: S554 covers it' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S545"
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
