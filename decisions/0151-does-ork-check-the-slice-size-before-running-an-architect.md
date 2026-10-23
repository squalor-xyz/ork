---
{
  "id": 151,
  "title": "Does ork check the slice size before running an architect?",
  "status": "open",
  "key": "slice.architect-size",
  "created": "2026-10-23",
  "context": "S08 runs the architect when the lineup has an architect entry. The matrix gives an architect row only for size L (slicer relay-roles recommend), and the slicer contract says the architect is optional and the owner decides.",
  "options": [
    {
      "label": "lineup-only",
      "summary": "Run an architect exactly when the plan's lineup has one; ork never reads the size",
      "tradeoffs": "Slicer's matrix stays the one size gate and the owner's lineup edit (S83) can add or drop it; ork cannot warn when an L slice has no architect."
    },
    {
      "label": "check-size",
      "summary": "Also compare the slice size and refuse or warn when they disagree",
      "tradeoffs": "Duplicates the matrix rule inside ork and adds a second gate to keep in step with slicer."
    }
  ],
  "recommendation": {
    "label": "lineup-only",
    "reasoning": "boundaries: slicer owns the matrix; the contract leaves the architect to the owner."
  },
  "refs": [
    "S08"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    }
  ]
}
---
