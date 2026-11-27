---
{
  "id": 654,
  "title": "How does the docs vs cli_test.go lint pair quoted doc strings with test strings?",
  "status": "decided",
  "key": "review.doc-cli-output-pairing",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S457 spec draft.",
  "options": [
    {
      "label": "digit-masked",
      "summary": "a doc span of 3+ words with a digit must match a cli_test.go literal once digits are masked"
    },
    {
      "label": "pair-table",
      "summary": "explicit pair table"
    },
    {
      "label": "doc-markers",
      "summary": "doc markers"
    }
  ],
  "recommendation": {
    "label": "digit-masked",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "digit-masked",
  "rationale": "Owner chose 'Digit-masked match' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S457"
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
