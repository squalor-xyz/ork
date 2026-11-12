---
{
  "id": 335,
  "title": "S358: at what context size does ork end a local role's session?",
  "status": "decided",
  "key": "roles.local-fresh-threshold",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "options": [
    {
      "label": "70k",
      "summary": "70k tokens, below the 83-86k where trial 2 compacted"
    },
    {
      "label": "60k",
      "summary": "60k tokens"
    },
    {
      "label": "setting",
      "summary": "a project setting, 70k by default"
    }
  ],
  "recommendation": {
    "label": "70k",
    "reasoning": "Trial 2 compacted at 83-86k."
  },
  "decision": "70k",
  "rationale": "Owner picked '70k' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S358"
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
