---
{
  "id": 606,
  "title": "Where does the main-write lock file live?",
  "status": "decided",
  "key": "landing.main-write-lock-path",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S614 spec draft.",
  "options": [
    {
      "label": "git-common-dir",
      "summary": "ork-main.lock in the git common directory (git rev-parse --git-common-dir), one per repository"
    },
    {
      "label": "state-dir",
      "summary": "under ork's state directory"
    }
  ],
  "recommendation": {
    "label": "git-common-dir",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "git-common-dir",
  "rationale": "Owner chose 'in .git' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S614"
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
