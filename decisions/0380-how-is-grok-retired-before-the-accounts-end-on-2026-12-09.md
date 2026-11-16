---
{
  "id": 380,
  "title": "How is grok retired before the accounts end on 2026-12-09?",
  "status": "decided",
  "key": "vendors.grok-cutover",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S374 spec check question.",
  "options": [
    {
      "label": "reorder-and-hold-out",
      "summary": "on 2026-12-08 the orchestrator runs ork vendor out grok --reason 'accounts end 2026-12-09'; S374 moves every grok row behind its fallback, marks it retired with the date, and files the same change for slicer's matrix; S374 runs on or about 2026-12-07 so grok stays first until then"
    },
    {
      "label": "delete",
      "summary": "delete the grok rows outright"
    }
  ],
  "recommendation": {
    "label": "reorder-and-hold-out",
    "reasoning": "ork vendor out with no --until holds across restarts"
  },
  "decision": "reorder-and-hold-out",
  "rationale": "Owner chose 'Reorder + hold out (Recommended)' and 'Hold until Dec 7 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S374"
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
