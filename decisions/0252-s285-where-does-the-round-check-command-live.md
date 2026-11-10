---
{
  "id": 252,
  "title": "S285: where does the round check command live?",
  "status": "decided",
  "key": "checks.round-command-home",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question for S285; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "registry: round_check_command, set with ork projects set NAME --round-check CMD, beside check_command"
    },
    {
      "label": "2",
      "summary": "slicer first: a slicer key (a slicer item first)"
    },
    {
      "label": "3",
      "summary": "both: slicer key first, registry as fallback"
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
