---
{
  "id": 536,
  "title": "What does the copy replace, rows and rules or rows only (Q2)?",
  "status": "decided",
  "key": "matrix.copy-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: What does the copy replace, rows and rules or rows only (Q2)?\nOptions:\n1. rows and rules: the target is replaced except the parts it keeps (Q3); true unification, but ork-specific rules u7 and u10 and slicer's `grok-4.7` pins land in projects they do not describe\n2. rows only: the target's rules stay; rule text that names row ids may then point at removed or wrong rows, so ork must warn or refuse\n3. merge: add the source rows the target lacks and keep the rest; keeps tuning but does not unify, and row order changes lineups\n4. rows plus named rules: `--rules u1,u3,...`; precise, but the owner picks rule ids on each copy\nRecommend: 1 - it is the unification the owner asked for on 2026-11-03; choose 4 if the ork-specific rules must not travel\nDecision key: matrix.copy-scope",
  "decision": "rows and rules: the target is replaced except the parts it keeps (Q3); true unification, but ork-specific rules u7 and u10 and slicer's `grok-4.7` pins land in projects they do not describe",
  "rationale": "Answered in ork on 2026-11-25: What does the copy replace, rows and rules or rows only (Q2)? (S261)",
  "refs": [
    "S261"
  ],
  "history": [
    {
      "date": "2026-11-24",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
