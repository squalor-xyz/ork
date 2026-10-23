---
{
  "id": 146,
  "title": "If one item fails to plan, does ork wave start abort the whole wave?",
  "status": "decided",
  "key": "wave.plan-failure",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-21",
  "decided_at": "2026-10-23",
  "context": "S60 landed with abort (internal/cli/wave.go:143-144). Record 21 covers failed runs only.",
  "options": [
    {
      "label": "abort",
      "summary": "Abort before anything starts, as now",
      "tradeoffs": "Nothing half-started; one bad item blocks the wave."
    },
    {
      "label": "continue",
      "summary": "Show the error and start the rest",
      "tradeoffs": "Matches decision 21 for run failures; a smaller wave than the owner chose may start."
    }
  ],
  "recommendation": {
    "label": "abort",
    "reasoning": "The owner confirmed the whole set; decision 21 covers runs that fail, not planning failures."
  },
  "decision": "abort",
  "rationale": "The owner confirmed the whole set; decision 21 covers runs that fail, not planning failures.",
  "refs": [
    "S60"
  ],
  "history": [
    {
      "date": "2026-10-21",
      "event": "ask"
    },
    {
      "date": "2026-10-23",
      "event": "decide"
    }
  ]
}
---
