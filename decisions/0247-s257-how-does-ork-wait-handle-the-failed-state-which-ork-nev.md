---
{
  "id": 247,
  "title": "S257: how does ork wait handle the failed state, which ork never produces?",
  "status": "decided",
  "key": "cli.wait-failed-state",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Raised by the S257 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "drop failed: leave failed out of wait state list for now; S311 can add it when a record state exists"
    },
    {
      "label": "2",
      "summary": "ledger field: add a ShowReply field from the slicer ledger run outcome"
    },
    {
      "label": "3",
      "summary": "map to stopped: report failed for a stopped run whose ledger outcome is not pass"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-09.",
  "refs": [
    "S257"
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
