---
{
  "id": 520,
  "title": "Does the read-only allowlist get rg?",
  "status": "decided",
  "key": "roles.read-only-rg",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "adj 457 dropped rg from the implementer presets because rg --pre=COMMAND runs a program; an allow glob cannot exclude options.",
  "options": [
    {
      "label": "drop",
      "summary": "Leave rg out; read-only roles use the Grep tool"
    },
    {
      "label": "allow",
      "summary": "Allow Bash(rg *) and tell roles not to pass --pre"
    },
    {
      "label": "supersede",
      "summary": "Allow rg and supersede adj 457 for read-only roles"
    }
  ],
  "recommendation": {
    "label": "drop",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "drop",
  "rationale": "Owner chose 'Leave rg out (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S505"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
