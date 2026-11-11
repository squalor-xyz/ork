---
{
  "id": 291,
  "title": "S329: what does a vendor connection error do?",
  "status": "decided",
  "key": "vendor.connection-error",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "resume-then-fallback",
      "summary": "new connection class; resume the same session once after 30 s; on a second failure fall back like error (adj 156); with no next row pause with one line; signals are only the two sampled ones (Grok reqwest send error, Claude api_error connection_failed)"
    },
    {
      "label": "error",
      "summary": "fall back at once, fresh session"
    },
    {
      "label": "keep-stopping",
      "summary": "stay unclassified, name the error in the pause"
    }
  ],
  "recommendation": {
    "label": "resume-then-fallback",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "resume-then-fallback",
  "rationale": "Owner picked Resume then fall back, One after 30s, and Only the two samples (all recommended).",
  "refs": [
    "S329"
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
