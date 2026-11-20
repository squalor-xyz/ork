---
{
  "id": 516,
  "title": "What does ork status --summary list under Next?",
  "status": "decided",
  "key": "status.summary-next",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S318 spec check 2026-11-20: slicer owns ordering (adj 239-241); slicer status --json returns one next; ork has no item score or spec-question predictor.",
  "options": [
    {
      "label": "slicer-order",
      "summary": "slicer's next, then the next unblocked items in slicer's own order; note open spec questions on items that already have them; no ork score"
    },
    {
      "label": "next-only",
      "summary": "Only slicer's next item"
    },
    {
      "label": "ork-ranks",
      "summary": "ork ranks by importance, urgency and effort"
    }
  ],
  "recommendation": {
    "label": "slicer-order",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "slicer-order",
  "rationale": "Owner chose 'Slicer's order (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
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
