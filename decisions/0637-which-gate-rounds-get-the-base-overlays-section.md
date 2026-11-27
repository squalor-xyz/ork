---
{
  "id": 637,
  "title": "Which gate rounds get the Base overlays section?",
  "status": "decided",
  "key": "baseoverlay.gate-rounds",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S594 spec draft.",
  "options": [
    {
      "label": "every-gate",
      "summary": "every gate round builds the section from the whole branch diff in its own temp dir"
    },
    {
      "label": "after-fix-only",
      "summary": "only after a fix round"
    }
  ],
  "recommendation": {
    "label": "every-gate",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "every-gate",
  "rationale": "Owner chose 'Every gate round' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S594"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
