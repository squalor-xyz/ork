---
{
  "id": 401,
  "title": "A TUI launch of a local run does not carve; what should the TUI do?",
  "status": "decided",
  "key": "ui.local-sliver-carve",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S359 rework question (run ork-20261116-210402-420a).",
  "options": [
    {
      "label": "pass-lineup",
      "summary": "the TUI's spec check and answer calls send the lineup, so a launch carves and re-plans like the CLI; needs a UI pass"
    },
    {
      "label": "leave",
      "summary": "refuse until the owner runs ork run once"
    },
    {
      "label": "refuse-early",
      "summary": "the TUI launch plan says the slice needs the CLI carve first"
    }
  ],
  "recommendation": {
    "label": "pass-lineup",
    "reasoning": "the owner's one-approval flow should hold in the TUI as well"
  },
  "decision": "pass-lineup",
  "rationale": "Owner chose 'Pass the lineup (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S359"
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
