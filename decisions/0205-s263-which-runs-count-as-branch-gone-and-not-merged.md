---
{
  "id": 205,
  "title": "S263: Which runs count as \"branch gone and not merged\"?",
  "status": "decided",
  "key": "landing.gone-branch-rule",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question drafted 2026-11-09 for S263; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "branch ref missing and worktree path gone, whether or not a commit is recorded (recommended: nothing is left to merge or clean, and it stops the repeats)"
    },
    {
      "label": "b",
      "summary": "branch ref missing only, even with the worktree still there (the worktree may hold unmerged work, so riskier)"
    },
    {
      "label": "c",
      "summary": "only runs with a recorded commit."
    }
  ],
  "recommendation": {
    "label": "a",
    "reasoning": "Drafter recommendation."
  },
  "decision": "a",
  "rationale": "Owner accepted the recommended answers for the drafted specs of S255, S257, S259, S263, S267, S283 and S293 on 2026-11-09.",
  "refs": [
    "S263"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
