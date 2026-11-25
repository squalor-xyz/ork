---
{
  "id": 571,
  "title": "Where must the help hint keep its scroll key, and what does the sweep assert where it cannot fit?",
  "status": "decided",
  "key": "ui.help-hint-scroll-floor",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S556 spec check: the keys-only form fits beside the close hint only above about 48 columns (adj 547, 552).",
  "options": [
    {
      "label": "floor",
      "summary": "assert pgup/pgdown wherever the keys-only form fits beside the close hint; below that the close hint alone, no q quit"
    },
    {
      "label": "always",
      "summary": "scroll key at every width from 30 up"
    },
    {
      "label": "as-decided",
      "summary": "assert only adj 547/552 plus scroll keys wherever they fit"
    }
  ],
  "recommendation": {
    "label": "floor",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "floor",
  "rationale": "Owner chose 'Floor' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S556"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
