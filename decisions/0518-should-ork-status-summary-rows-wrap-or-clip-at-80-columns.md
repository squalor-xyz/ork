---
{
  "id": 518,
  "title": "Should ork status --summary rows wrap or clip at 80 columns?",
  "status": "decided",
  "key": "ui.status-summary-width",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S318 UI pass 2026-11-20: rows ran to 98-167 columns with real-length data.",
  "options": [
    {
      "label": "hanging-indent",
      "summary": "Wrap text rows at 80 with a 4-space continuation indent; JSON unchanged"
    },
    {
      "label": "as-is",
      "summary": "Plain rows; the terminal soft-wraps"
    },
    {
      "label": "clip-title",
      "summary": "Clip titles and check text with ..."
    }
  ],
  "recommendation": {
    "label": "hanging-indent",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "hanging-indent",
  "rationale": "Owner chose 'Wrap with indent (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S318"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
