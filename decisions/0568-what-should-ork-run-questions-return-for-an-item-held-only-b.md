---
{
  "id": 568,
  "title": "What should ork run --questions return for an item held only by a Check finding?",
  "status": "decided",
  "key": "cli.questions-check-finding-exit",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S563 finding: such an item returns ExitWaitQuestion though it has no owner question.",
  "options": [
    {
      "label": "skip",
      "summary": "return ExitOK and print the held line"
    },
    {
      "label": "new-code",
      "summary": "add a new exit code"
    },
    {
      "label": "as-written",
      "summary": "keep ExitWaitQuestion"
    }
  ],
  "recommendation": {
    "label": "skip",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "skip",
  "rationale": "Owner chose 'Skip: exit OK' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S572"
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
