---
{
  "id": 124,
  "title": "Where do slivers run?",
  "status": "decided",
  "key": "slice.sliver-worktree",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "docs/design.md:185 says slivers run as their own items; slicer says they run in the parent's worktree.",
  "options": [
    {
      "label": "parent-worktree",
      "summary": "Slivers share the parent's worktree and branch and land once; they run one after another and count against the caps"
    }
  ],
  "recommendation": {
    "label": "parent-worktree",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "parent-worktree",
  "rationale": "Owner 2026-10-20: follows the slicer contract (landing.md:29-31); docs/design.md:185 (their own items) is to be corrected. The worktree guard already blocks parallel runs on one worktree.",
  "refs": [
    "S62,S61"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
