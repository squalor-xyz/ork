---
{
  "id": 181,
  "title": "What are the defaults and the fallback for local rows?",
  "status": "decided",
  "key": "roles.local-model.defaults",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "S301.",
  "options": [
    {
      "label": "opt-in-auto-fallback",
      "summary": "no role defaults to local; fallback to the next cloud row is automatic when the endpoint is unreachable; one local model at a time"
    },
    {
      "label": "fallback-asks",
      "summary": "an unreachable endpoint pauses the run with a question"
    }
  ],
  "recommendation": {
    "label": "opt-in-auto-fallback",
    "reasoning": "Matches S301's recommendations and adj 156."
  },
  "decision": "opt-in-auto-fallback",
  "rationale": "Owner confirmed all three, interactive question, 2026-11-06.",
  "refs": [
    "S301",
    "adj:156"
  ],
  "history": [
    {
      "date": "2026-11-06",
      "event": "ask"
    },
    {
      "date": "2026-11-06",
      "event": "decide"
    }
  ]
}
---
