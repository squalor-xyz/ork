---
{
  "id": 332,
  "title": "Does a local role start a fresh session instead of auto-compacting?",
  "status": "decided",
  "key": "roles.local-fresh-session",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "chat, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "S304 trials 1 and 2 (2026-11-11/12).",
  "options": [
    {
      "label": "fresh",
      "summary": "ork ends the session past a threshold and starts a new one with the brief and the role's progress"
    },
    {
      "label": "compact",
      "summary": "keep auto-compaction"
    }
  ],
  "recommendation": {
    "label": "fresh",
    "reasoning": "Compaction on the local server costs 15-19 min; a fresh start costs seconds."
  },
  "decision": "fresh",
  "rationale": "Owner: '2 is a great idea. file it'; recorded by the agent as the owner's delegate (adj 316).",
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
