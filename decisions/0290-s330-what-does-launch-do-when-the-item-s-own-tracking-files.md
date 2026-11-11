---
{
  "id": 290,
  "title": "S330: what does launch do when the item's own tracking files are uncommitted on main?",
  "status": "decided",
  "key": "launch.tracking-commit",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "commit-first",
      "summary": "commit the .slicer tracking edits on main by path, then create the worktree"
    },
    {
      "label": "refuse",
      "summary": "refuse with one line naming the files and the commit command"
    },
    {
      "label": "warn",
      "summary": "warn and launch"
    }
  ],
  "recommendation": {
    "label": "commit-first",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "commit-first",
  "rationale": "Owner picked Commit them first (recommended).",
  "refs": [
    "S330"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
