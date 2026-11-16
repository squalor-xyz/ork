---
{
  "id": 375,
  "title": "What does ork's read record hold for a Grep, given the hook sees the call but not its result?",
  "status": "decided",
  "key": "roles.local-read-log-content",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S371 spec check question.",
  "options": [
    {
      "label": "calls-only",
      "summary": "one line per Read (path:start-end) and per Grep (pattern in path); identical entries merge, newest last, at most 80 lines"
    },
    {
      "label": "hit-counts",
      "summary": "plus ': N hits' on each Grep, from a new result source"
    },
    {
      "label": "hit-lines",
      "summary": "plus the first hit lines, from a new result source"
    }
  ],
  "recommendation": {
    "label": "calls-only",
    "reasoning": "the approval hook runs before the call (PreToolUse only)"
  },
  "decision": "calls-only",
  "rationale": "Owner chose 'Calls only (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S371"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
