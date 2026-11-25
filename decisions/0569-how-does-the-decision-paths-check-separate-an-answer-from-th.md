---
{
  "id": 569,
  "title": "How does the decision-paths check separate an answer from the next question's title in the Owner decisions paragraph?",
  "status": "decided",
  "key": "spec.owner-decisions-format",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S560 finding: question titles and unchosen options can be flagged as paths.",
  "options": [
    {
      "label": "one-line-each",
      "summary": "RecordSpecAnswers writes each title -> answer [key]. on its own line; scan only text after ->"
    },
    {
      "label": "segment-after-arrow",
      "summary": "drop only text before the first ->"
    },
    {
      "label": "leave-as-is",
      "summary": "retire S573"
    }
  ],
  "recommendation": {
    "label": "one-line-each",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "one-line-each",
  "rationale": "Owner chose 'One line each' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S573"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
