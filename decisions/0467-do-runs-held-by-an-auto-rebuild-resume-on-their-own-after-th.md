---
{
  "id": 467,
  "title": "Do runs held by an auto-rebuild resume on their own after the restart?",
  "status": "decided",
  "key": "selfhost.auto-rebuild-resume",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S431 implementer (2026-11-18): step 4 leaves held runs to today's recovery, which pauses each as interrupted for the owner.",
  "options": [
    {
      "label": "recovery",
      "summary": "today's recovery; the owner retries each held run"
    },
    {
      "label": "auto-resume",
      "summary": "mark runs held at a rebuild in their record; the successor retries them at start with no owner action (a small follow-up slice: record marker and start-up retry)"
    }
  ],
  "recommendation": {
    "label": "auto-resume",
    "reasoning": "The goal is a rebuild with no hand command."
  },
  "decision": "auto-resume",
  "rationale": "Owner chose 'Auto-resume (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S431"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
