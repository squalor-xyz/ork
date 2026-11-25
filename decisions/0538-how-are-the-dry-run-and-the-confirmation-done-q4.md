---
{
  "id": 538,
  "title": "How are the dry run and the confirmation done (Q4)?",
  "status": "decided",
  "key": "matrix.copy-confirm",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: How are the dry run and the confirmation done (Q4)?\nOptions:\n1. per project: always a dry run first, show slicer's steps and warnings, ask `[y/N]` for each project (Enter is No); `--yes` skips the questions; `--dry-run` stops after the preview\n2. one question: a single confirmation for all projects (fewer prompts; one yes changes every project)\n3. no question: only `--dry-run` (simplest; a typo writes every project)\nRecommend: 1 - it matches the spec's tests and limits a mistaken copy to one project\nDecision key: matrix.copy-confirm",
  "decision": "per project: always a dry run first, show slicer's steps and warnings, ask `[y/N]` for each project (Enter is No); `--yes` skips the questions; `--dry-run` stops after the preview",
  "rationale": "Answered in ork on 2026-11-25: How are the dry run and the confirmation done (Q4)? (S261)",
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
