---
{
  "id": 463,
  "title": "How does the held refusal line for non-terminal spec answers fit 80 columns?",
  "status": "decided",
  "key": "cli.held-line",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S404 implementer (2026-11-18): the spec's held: refusal line is 88 columns and splits inside --answer KEY=LABEL at 80 columns.",
  "options": [
    {
      "label": "keep",
      "summary": "the line as the spec gives it"
    },
    {
      "label": "shorten",
      "summary": "held: S1: input is not a terminal; answer with --answer KEY=LABEL, with tests and docs updated"
    },
    {
      "label": "wrap",
      "summary": "wrap the line with wrapLines at 80 and loosen the tests"
    }
  ],
  "recommendation": {
    "label": "shorten",
    "reasoning": "Fits 80 columns with no wrap logic; the hint stays one piece."
  },
  "decision": "shorten",
  "rationale": "Owner chose 'Shorten the line (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S404"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
