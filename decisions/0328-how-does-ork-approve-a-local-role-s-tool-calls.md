---
{
  "id": 328,
  "title": "How does ork approve a local role's tool calls?",
  "status": "decided",
  "key": "roles.local-approval",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "chat, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "S304 trial 2, 2026-11-12. Owner: 'it can't be auto, it needs to be manual -- and ork should approve. the auto classifier does not work yet'.",
  "options": [
    {
      "label": "full",
      "summary": "a local role runs in manual mode and ork allows, denies with a reason, or asks the owner per call; whole-file reads over 500 lines are denied"
    },
    {
      "label": "read-cap",
      "summary": "only a read-size and path cap; the allowlist stays the only approval"
    }
  ],
  "recommendation": {
    "label": "full",
    "reasoning": "The auto classifier does not work on the local server yet; the brief paragraph did not change what the model read (S304 trial 2)."
  },
  "decision": "full",
  "rationale": "Owner: 'yes file it, full approval point, 500 lines'; recorded by the agent as the owner's delegate (adj 316).",
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
