---
{
  "id": 459,
  "title": "Which stopped runs can ork retry re-land?",
  "status": "decided",
  "key": "land.retry-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S408",
  "options": [
    {
      "label": "land-only",
      "summary": "Only a committed run stopped in the land stage with its worktree and branch"
    },
    {
      "label": "any-committed",
      "summary": "Any stopped run with a commit whose worktree and branch exist"
    }
  ],
  "recommendation": {
    "label": "land-only",
    "reasoning": "narrowest change"
  },
  "decision": "land-only",
  "rationale": "Owner chose 'Stopped at land only' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
