---
{
  "id": 539,
  "title": "What does apply-to-all do when a project fails (Q5)?",
  "status": "decided",
  "key": "matrix.copy-all",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: What does apply-to-all do when a project fails (Q5)?\nOptions:\n1. check all first: dry-run every target and write nothing if any dry run fails; then write each project with `expect`, carry on after a write failure, report each status, `partial: true`, exit 1\n2. stop at first failure: write project by project and stop at the first failure\n3. write the passing ones: no pre-check; write every project that passes and skip the failures\nRecommend: 1 - a source-side problem is found once, and each project is still atomic on its own\nDecision key: matrix.copy-all",
  "decision": "check all first: dry-run every target and write nothing if any dry run fails; then write each project with `expect`, carry on after a write failure, report each status, `partial: true`, exit 1",
  "rationale": "Answered in ork on 2026-11-25: What does apply-to-all do when a project fails (Q5)? (S261)",
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
