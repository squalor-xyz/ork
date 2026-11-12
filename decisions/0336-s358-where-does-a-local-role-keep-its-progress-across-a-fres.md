---
{
  "id": 336,
  "title": "S358: where does a local role keep its progress across a fresh session?",
  "status": "decided",
  "key": "roles.local-progress-file",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
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
  "decision": "scratch-note",
  "rationale": "Owner picked 'scratch-note' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S358"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
