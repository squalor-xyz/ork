---
{
  "id": 275,
  "title": "S226: what does the screen do when a matrix.edit reply arrives after esc?",
  "status": "decided",
  "key": "ui.matrix-editor-cancel-run",
  "asked_by": "Claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S226 UI pass; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "ignore",
      "summary": "a reply for a closed form changes nothing"
    },
    {
      "label": "reload",
      "summary": "reload roles.matrix and show Edited ID (uncommitted)"
    },
    {
      "label": "refuse",
      "summary": "esc does nothing while a run is out"
    }
  ],
  "recommendation": {
    "label": "reload",
    "reasoning": "UI pass recommendation."
  },
  "decision": "reload",
  "rationale": "Owner picked reload on 2026-11-11.",
  "refs": [
    "S226"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
