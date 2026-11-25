---
{
  "id": 545,
  "title": "Should the copy also keep the rows that the target picks through its spec_check_rows/ui_pass_rows settings and its Local trial rules?",
  "status": "decided",
  "key": "matrix.copy-settings-rows",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: Should the copy also keep the rows that the target picks through its spec_check_rows/ui_pass_rows settings and its Local trial rules?\nOptions:\n1. keep-settings: the handler also keeps by id every row listed in the target's spec_check_rows/ui_pass_rows, and in its Local rules\n2. refuse: a target whose settings list a row the copy would remove fails with copy_conflict\n3. warn: copy as now, and warn when a setting names a row that the copy removes\nRecommend: 1 - it matches \"keeps its own\" and keeps the target's runs working; the planner already takes kept ids\nDecision key: matrix.copy-settings-rows",
  "decision": "1 - keep-settings: also keep by id every row listed in the target's spec_check_rows/ui_pass_rows and its Local rules (owner Jon, 2026-11-24)",
  "rationale": "Answered in ork on 2026-11-25: Should the copy also keep the rows that the target picks through its spec_check_rows/ui_pass_rows settings and its Local trial rules?",
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
