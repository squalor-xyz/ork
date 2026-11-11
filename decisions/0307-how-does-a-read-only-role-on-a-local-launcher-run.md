---
{
  "id": 307,
  "title": "How does a read-only role on a local launcher run?",
  "status": "decided",
  "key": "roles.local-read-only-mode",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "read-only-allowlist",
      "summary": "A read-only allowlist with --permission-prompts none, no plan mode"
    },
    {
      "label": "plan-mode",
      "summary": "Keep --permission-mode plan as for cloud"
    }
  ],
  "recommendation": {
    "label": "read-only-allowlist",
    "reasoning": "plan mode sends Bash calls to the model-backed classifier, which timed out on the local model in the S304 trial"
  },
  "decision": "read-only-allowlist",
  "rationale": "Owner: the model backed classifier does not work yet, so for local we should not use plan permission mode",
  "refs": [
    "S343",
    "S304"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
