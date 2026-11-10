---
{
  "id": 260,
  "title": "S295: how is a check command stored with --no-verify handled at run setup?",
  "status": "decided",
  "key": "check.no-verify-at-setup",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-10",
  "created": "2026-11-10",
  "decided_at": "2026-11-10",
  "context": "Raised by the S295 implementer; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "accept as is: the owner fixes the command before ork retry"
    },
    {
      "label": "2",
      "summary": "remember --no-verify: store no_verify_check in the registry entry so setup skips verification; a changed --check without --no-verify clears it"
    },
    {
      "label": "3",
      "summary": "ork retry ID --force continues past the setup pause for that run only"
    }
  ],
  "recommendation": {
    "label": "2",
    "reasoning": "Implementer recommendation."
  },
  "decision": "2",
  "rationale": "Owner picked option 2 on 2026-11-10.",
  "refs": [
    "S295"
  ],
  "history": [
    {
      "date": "2026-11-10",
      "event": "ask"
    },
    {
      "date": "2026-11-10",
      "event": "decide"
    }
  ]
}
---
