---
{
  "id": 336,
  "title": "S358: where does a local role keep its progress across a fresh session?",
  "status": "open",
  "key": "roles.local-progress-file",
  "asked_by": "claude",
  "created": "2026-11-12",
  "options": [
    {
      "label": "scratch-note",
      "summary": "a progress note in .scratch/progress-RUN-ROLE.md, carried with git diff --stat into the new brief"
    },
    {
      "label": "diff-only",
      "summary": "only git diff --stat and the changed files' diff"
    },
    {
      "label": "worktree-note",
      "summary": "a note file in the worktree that the finish step removes"
    }
  ],
  "recommendation": {
    "label": "scratch-note",
    "reasoning": "The plan and open problems are not in the diff."
  },
  "refs": [
    "S358"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    }
  ]
}
---
