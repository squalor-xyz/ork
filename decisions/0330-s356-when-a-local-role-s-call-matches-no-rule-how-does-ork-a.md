---
{
  "id": 330,
  "title": "S356: when a local role's call matches no rule, how does ork ask the owner?",
  "status": "decided",
  "key": "roles.local-approval-ask",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "options": [
    {
      "label": "block",
      "summary": "the hook blocks and the run shows a permission question (ork answer / run pane); deny on a long timeout"
    },
    {
      "label": "deny-record",
      "summary": "deny at once and record the call for the owner to allow on retry (S346's per-run grant)"
    }
  ],
  "recommendation": {
    "label": "block",
    "reasoning": "The role goes on with the owner's answer instead of failing a round."
  },
  "decision": "block",
  "rationale": "Owner picked 'block' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
