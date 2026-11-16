---
{
  "id": 378,
  "title": "How should a soft wrap inside a permission detail be told apart from a real newline or a space?",
  "status": "decided",
  "key": "ui.permission-detail-wrap",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S373 question (run ork-20261116-155524-4cf4).",
  "options": [
    {
      "label": "hang",
      "summary": "soft-wrap continuation lines get a 2-space hanging indent via hangLines"
    },
    {
      "label": "glyph",
      "summary": "end each soft-wrapped line with an ASCII continuation mark"
    },
    {
      "label": "keep",
      "summary": "plain wrap as shipped"
    }
  ],
  "recommendation": {
    "label": "hang",
    "reasoning": "reuses an existing helper, costs no right-edge columns, works in ASCII mode"
  },
  "decision": "hang",
  "rationale": "Owner chose 'Hanging indent (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S373"
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
