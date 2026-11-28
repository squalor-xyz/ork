---
{
  "id": 670,
  "title": "Which inserted lines does the doc blank-line lint count?",
  "status": "decided",
  "key": "review.doc-blank-line-inserted-lines",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S647 spec draft.",
  "options": [
    {
      "label": "pure-insertions",
      "summary": "only hunks with no deleted line; flag when the first inserted line touches a non-blank line before it or the last touches one after it"
    },
    {
      "label": "skip-extensions",
      "summary": "skip paragraph extensions"
    },
    {
      "label": "any-added",
      "summary": "any added line"
    }
  ],
  "recommendation": {
    "label": "pure-insertions",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pure-insertions",
  "rationale": "Owner chose 'Pure insertions' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S647"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
