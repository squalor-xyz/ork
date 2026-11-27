---
{
  "id": 625,
  "title": "How many importer-package commands may the Base overlays section list?",
  "status": "decided",
  "key": "base-overlays.importer-limit",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S620 spec draft.",
  "options": [
    {
      "label": "cap-20",
      "summary": "reuse baseOverlayLimit for all importer lines with an N more left out line"
    },
    {
      "label": "no-cap",
      "summary": "no cap"
    },
    {
      "label": "per-file",
      "summary": "20 per reverted file"
    }
  ],
  "recommendation": {
    "label": "cap-20",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "cap-20",
  "rationale": "Owner chose 'Cap at 20' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S620"
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
