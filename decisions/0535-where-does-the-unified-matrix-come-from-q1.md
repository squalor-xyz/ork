---
{
  "id": 535,
  "title": "Where does the unified matrix come from (Q1)?",
  "status": "decided",
  "key": "matrix.copy-source",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: Where does the unified matrix come from (Q1)?\nOptions:\n1. named project: `ork roles matrix copy FROM ...`, the owner names the source on each call (no new state)\n2. fixed ork: always the registered project `ork` (shortest command, hard-codes a registry name)\n3. stored master: a master matrix that ork stores (a new state file, a boundary change against `lineup.source` adj 24, and its own editor)\nRecommend: 1 - no new state and no boundary change; `from` is already the wire shape the spec's tests assume\nDecision key: matrix.copy-source",
  "decision": "named project: `ork roles matrix copy FROM ...`, the owner names the source on each call (no new state)",
  "rationale": "Answered in ork on 2026-11-25: Where does the unified matrix come from (Q1)? (S261)",
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
