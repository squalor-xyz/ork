---
{
  "id": 357,
  "title": "When do the slice's own Check rows run, and are they re-run in fix rounds?",
  "status": "decided",
  "key": "checks.slice-check-schedule",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S317 spec check question.",
  "options": [
    {
      "label": "before-gate",
      "summary": "once when the state is about to go to the gate, again only if a later fix round changed files outside docs and tracking"
    },
    {
      "label": "first-green",
      "summary": "once right after the first green round check, like checks-full"
    },
    {
      "label": "every-green",
      "summary": "after every green round check, skipping a fix round that changed no product file"
    }
  ],
  "recommendation": {
    "label": "before-gate",
    "reasoning": "matches adj 236 (once before the gate, not every round)"
  },
  "decision": "before-gate",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-15 ('Before gate (Recommended)').",
  "refs": [
    "S317"
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
