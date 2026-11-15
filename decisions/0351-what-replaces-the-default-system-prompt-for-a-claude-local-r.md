---
{
  "id": 351,
  "title": "What replaces the default system prompt for a claude-local role?",
  "status": "decided",
  "key": "roles.local-system-prompt",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, session 8d711f67",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "options": [
    {
      "label": "ork-prompt",
      "summary": "ork's own short system prompt file (under 2,000 chars); role prompt and brief stay the user message"
    },
    {
      "label": "default-trimmed",
      "summary": "default prompt with --exclude-dynamic-system-prompt-sections"
    }
  ],
  "recommendation": {
    "label": "ork-prompt",
    "reasoning": "See S364."
  },
  "decision": "ork-prompt",
  "rationale": "Owner picked ork-prompt in an interactive question, ork relay session 8d711f67, 2026-11-15; recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S364"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
