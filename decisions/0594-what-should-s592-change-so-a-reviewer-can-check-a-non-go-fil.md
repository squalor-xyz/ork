---
{
  "id": 594,
  "title": "What should S592 change so a reviewer can check a non-Go file against its base version?",
  "status": "decided",
  "key": "roles.reviewer-git-grep",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S592 tool feedback; git grep -O runs a program as a pager.",
  "options": [
    {
      "label": "no-allowlist-change",
      "summary": "add no entry; tell reviewers in the Base overlays text to read non-Go base files with git show"
    },
    {
      "label": "git-grep",
      "summary": "add git grep entries"
    },
    {
      "label": "git-grep-reviewer-only",
      "summary": "reviewer-only allowlist"
    }
  ],
  "recommendation": {
    "label": "no-allowlist-change",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "no-allowlist-change",
  "rationale": "Owner chose 'No allowlist change' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S592"
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
