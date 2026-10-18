---
{
  "id": 47,
  "title": "How is ork tested?",
  "status": "decided",
  "key": "testing.strategy",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Real runs cost money.",
  "options": [
    {
      "label": "fakes-fixtures",
      "summary": "Fake CLIs and fixtures"
    },
    {
      "label": "plus-live",
      "summary": "Plus an opt-in live smoke test"
    }
  ],
  "decision": "fakes-fixtures",
  "rationale": "Owner interview 2026-10-18: fake vendor CLIs replaying captured JSON, recorded slicer/adj output, TUI as pure model updates; real runs on a manual checklist.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
