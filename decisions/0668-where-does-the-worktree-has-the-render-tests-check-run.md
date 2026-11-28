---
{
  "id": 668,
  "title": "Where does the worktree-has-the-render-tests check run?",
  "status": "decided",
  "key": "ui.brief-render-detection-site",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S660 spec draft.",
  "options": [
    {
      "label": "caller-detects",
      "summary": "round.go checks the worktree and sets two BriefInput booleans; BuildBrief stays pure"
    },
    {
      "label": "buildbrief-detects",
      "summary": "BuildBrief reads the worktree"
    }
  ],
  "recommendation": {
    "label": "caller-detects",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "caller-detects",
  "rationale": "Owner chose 'Caller detects' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S660"
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
