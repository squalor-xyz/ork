---
{
  "id": 686,
  "title": "How does a grok UI pass get a renders-only write rule?",
  "status": "decided",
  "key": "ui.renders-dir-grant-grok",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S667 spec draft; grok ends 2026-12-09.",
  "options": [
    {
      "label": "defer-grok",
      "summary": "the grok brief keeps the vendor gate; the lint row omits grok and grok-local"
    },
    {
      "label": "probe-sandbox",
      "summary": "probe --sandbox"
    },
    {
      "label": "unscoped",
      "summary": "unscoped write rule"
    }
  ],
  "recommendation": {
    "label": "defer-grok",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "defer-grok",
  "rationale": "Owner chose 'Defer grok' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S667"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
