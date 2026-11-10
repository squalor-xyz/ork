---
{
  "id": 253,
  "title": "S285: may a project set a round command without a full check_command?",
  "status": "decided",
  "key": "checks.round-command-needs-full",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question for S285; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "no: projects set refuses --round-check while check_command is empty"
    },
    {
      "label": "2",
      "summary": "yes: rounds run the round command; full checks run only slicer check"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check/spec recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-09.",
  "refs": [
    "S285"
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
