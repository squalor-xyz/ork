---
{
  "id": 123,
  "title": "When does the architect run, relative to the reviewer and gate?",
  "status": "decided",
  "key": "slice.architect-order",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "docs/design.md says the architect runs alongside the reviewer; the contract (slicer ai relay) says after findings, before the gate.",
  "options": [
    {
      "label": "after-findings",
      "summary": "Follow the slicer contract: the architect runs after accepted findings and before the gate"
    }
  ],
  "recommendation": {
    "label": "after-findings",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "after-findings",
  "rationale": "Owner 2026-10-20: ork implements slicer's relay contract; docs/design.md:191-193 (alongside the reviewer) is to be corrected.",
  "refs": [
    "S08,S61"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
