---
{
  "id": 196,
  "title": "How are the r33 local rows written in the matrix, now that adj 194 (2026-11-09) makes local explicit-lineup only?",
  "status": "decided",
  "key": "roles.local-model.matrix-condition",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Question: How are the r33 local rows written in the matrix, now that adj 194 (2026-11-09) makes local explicit-lineup only?\nOptions:\n1. no-auto-pick: three rows (claude-local, codex-local, grok-local) with a condition that slicer's `recommend` never matches on its own, so a run reaches them only through `--select` or the lineup override (`ork run` lineup flags)\n2. fallback-only: rows with the size S and well-specified condition, placed after the cloud rows, so they appear only as fallbacks in the chain and are never first\n3. as written: keep the \"size S and well specified, picked first\" condition of 2026-11-07, which contradicts adj 194\nRecommend: 1 - adj 194 says a run uses a local row only when the owner picks it, and a row that matches automatically would break that.\nDecision key: roles.local-model.matrix-condition",
  "decision": "fallback-only: rows with the size S and well-specified condition, placed after the cloud rows, so they appear only as fallbacks in the chain and are never first",
  "rationale": "Answered in ork on 2026-11-09: How are the r33 local rows written in the matrix, now that adj 194 (2026-11-09) makes local explicit-lineup only?",
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
