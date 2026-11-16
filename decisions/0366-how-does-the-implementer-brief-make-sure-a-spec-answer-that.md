---
{
  "id": 366,
  "title": "How does the implementer brief make sure a spec answer that changes the slice's text is applied first?",
  "status": "decided",
  "key": "engine.spec-answer-apply",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S368 spec check question.",
  "options": [
    {
      "label": "every-answer",
      "summary": "no detection; the first 'Owner decisions' section gains one sentence: apply any answer that changes the slice's Check or Failing tests text with slicer edit before any code"
    },
    {
      "label": "keyword",
      "summary": "match the word Check and put matches in a new first section"
    },
    {
      "label": "tighten-label",
      "summary": "only answers labelled tighten or human check"
    }
  ],
  "recommendation": {
    "label": "every-answer",
    "reasoning": "needs no matching rule and still puts the edit first"
  },
  "decision": "every-answer",
  "rationale": "Owner chose 'Every answer (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S368"
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
