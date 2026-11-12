---
{
  "id": 318,
  "title": "S344: how does ork learn the UI pass row order?",
  "status": "decided",
  "key": "roles.ui-pass-rows",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-11",
  "decided_at": "2026-11-12",
  "context": "Today UIPassRowID takes one row, the first rule match (internal/slicercli/write.go:374-386).",
  "options": [
    {
      "label": "project-setting",
      "summary": "a project setting ui_pass_rows, the same shape and rules as spec_check_rows (S258)"
    },
    {
      "label": "rule-list",
      "summary": "a rule names several 'UI pass row (rNN)' ids, in order"
    }
  ],
  "recommendation": {
    "label": "project-setting",
    "reasoning": "It mirrors spec_check_rows, and a trial can change it without a matrix rule edit."
  },
  "decision": "project-setting",
  "rationale": "Owner picked 'project-setting' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S344"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
