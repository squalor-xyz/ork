---
{
  "id": 242,
  "title": "S301: how parallel local roles use a local run whole-run reservation",
  "status": "decided",
  "key": "roles.local-model.pool-accounting",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Raised by the S301 implementer (ork-20261109-130828-3727); owner answered in an interactive question, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "RESERVED-ONE: the run reserves one shared slot; its local roles use it one at a time"
    },
    {
      "label": "2",
      "summary": "RESERVED-PLUS: one role uses the reservation; extra simultaneous local roles take additional shared slots"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Implementer recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked the recommended option for S301 on 2026-11-09.",
  "refs": [
    "S301"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
