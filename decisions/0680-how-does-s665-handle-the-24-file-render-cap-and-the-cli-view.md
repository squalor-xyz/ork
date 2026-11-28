---
{
  "id": 680,
  "title": "How does S665 handle the 24-file render cap and the CLI/VIEW naming?",
  "status": "decided",
  "key": "ui.render-file-cap",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S665 spec check.",
  "options": [
    {
      "label": "reword-warn",
      "summary": "VIEW-WxH naming only for views the brief's render sections do not cover; tell the UI role to keep at most 24 files in ui.md and the brief; reword the either-section fallback"
    },
    {
      "label": "raise-cap",
      "summary": "raise maxRenders"
    },
    {
      "label": "naming-only",
      "summary": "naming only"
    }
  ],
  "recommendation": {
    "label": "reword-warn",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "reword-warn",
  "rationale": "Owner chose 'Reword + warn about cap' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S665"
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
