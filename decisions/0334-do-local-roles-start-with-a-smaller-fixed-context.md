---
{
  "id": 334,
  "title": "Do local roles start with a smaller fixed context?",
  "status": "decided",
  "key": "roles.local-small-context",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "chat, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "S304 trials 1 and 2 (2026-11-11/12).",
  "options": [
    {
      "label": "small",
      "summary": "turn off unused tools and slash commands and cap long command output for local roles"
    },
    {
      "label": "same",
      "summary": "same context as cloud"
    }
  ],
  "recommendation": {
    "label": "small",
    "reasoning": "Fixed context is prefilled every turn and counts toward compaction."
  },
  "decision": "small",
  "rationale": "Owner: '4 yes for local'; recorded by the agent as the owner's delegate (adj 316).",
  "refs": [
    "S360"
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
