---
{
  "id": 561,
  "title": "Which fields does a set step's line print after its id?",
  "status": "decided",
  "key": "ui.matrix-copy-step-fields",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Which fields does a set step's line print after its id?\nOptions:\n1. changed: print only the fields of the row in plan.set() that differ from the target's current row, as key=value in the fixed order role, when, agent, vendor, model, reasoning, note\n2. all: print every non-empty field of that row, in the same order\nRecommend: 1 - the line then shows what the copy changes, and a step with no differences prints the id alone\nDecision key: ui.matrix-copy-step-fields",
  "decision": "changed: print only the fields of the row in plan.set() that differ from the target's current row, as key=value in the fixed order role, when, agent, vendor, model, reasoning, note",
  "rationale": "Answered in ork on 2026-11-25: Which fields does a set step's line print after its id? (S547)",
  "refs": [
    "S547"
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
