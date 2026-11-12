---
{
  "id": 331,
  "title": "S356: does ork pass --permission-mode manual for a local role itself?",
  "status": "decided",
  "key": "roles.local-permission-mode",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "options": [
    {
      "label": "ork",
      "summary": "ork passes --permission-mode manual for a local role; the launcher's own flag is not needed"
    },
    {
      "label": "launcher",
      "summary": "rely on the launcher's flag"
    }
  ],
  "recommendation": {
    "label": "ork",
    "reasoning": "The mode is then ork's whatever a launcher sets."
  },
  "decision": "ork",
  "rationale": "Owner picked 'ork' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
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
