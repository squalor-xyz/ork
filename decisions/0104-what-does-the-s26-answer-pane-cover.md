---
{
  "id": 104,
  "title": "What does the S26 answer pane cover?",
  "status": "decided",
  "key": "ui.answer-pane-scope",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Engine.Answer works only on a paused run with a question; Pending holds question text and no options.",
  "options": [
    {
      "label": "three-cases",
      "summary": "paused with a question answers; paused without one retries; stopped is read-only; no options",
      "tradeoffs": "no engine change"
    },
    {
      "label": "options",
      "summary": "add structured options to the report, Pending and the pane",
      "tradeoffs": "touches report parsing, runrec, wire and prompts; likely L"
    }
  ],
  "recommendation": {
    "label": "three-cases",
    "reasoning": "No engine change; options can follow when a real need shows."
  },
  "decision": "three-cases",
  "rationale": "Owner 2026-10-20: pane for the three cases, no structured options.",
  "refs": [
    "S26",
    "S44"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
