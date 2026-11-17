---
{
  "id": 405,
  "title": "When is the spec-check directory of a commit-stopped run removed?",
  "status": "decided",
  "key": "speccheck.merged-cleanup",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S229 spec question, 2026-11-17.",
  "options": [
    {
      "label": "on-merge-found",
      "summary": "the HEAD check that finds the branch merged removes it"
    },
    {
      "label": "at-cleanup",
      "summary": "only at dismissal or the 14-day sweep"
    }
  ],
  "recommendation": {
    "label": "on-merge-found",
    "reasoning": "no stale directories; guarded against restart and git errors"
  },
  "decision": "on-merge-found",
  "rationale": "Owner chose 'When merge is found (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S229"
  ],
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
