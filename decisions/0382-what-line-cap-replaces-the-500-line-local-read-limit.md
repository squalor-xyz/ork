---
{
  "id": 382,
  "title": "What line cap replaces the 500-line local Read limit?",
  "status": "decided",
  "key": "roles.local-read-cap",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S372 spec check question.",
  "options": [
    {
      "label": "150",
      "summary": "every local Read capped at 150 lines; the denial says to Grep for the symbol and Read 150 lines around it"
    },
    {
      "label": "250",
      "summary": "cap 250, today's wording"
    },
    {
      "label": "split",
      "summary": "the local implementer is capped at 150 lines with the Grep-and-window denial; local read-only roles stay at 500 with today's denial"
    }
  ],
  "recommendation": {
    "label": "150",
    "reasoning": "trial 5 filled the window with 500-line chunks"
  },
  "decision": "split",
  "rationale": "Owner chose 'Split' (not the recommendation) in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S372"
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
