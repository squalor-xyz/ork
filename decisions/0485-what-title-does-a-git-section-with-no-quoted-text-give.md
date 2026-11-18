---
{
  "id": 485,
  "title": "What title does a Git section with no quoted text give?",
  "status": "decided",
  "key": "git.commit-title",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "internal/engine/title.go:28-43; S139 took the first line.",
  "options": [
    {
      "label": "id-title",
      "summary": "Only a double-quoted title counts; otherwise ID: title"
    },
    {
      "label": "first-line",
      "summary": "Keep the first line unless it reads like a sentence"
    },
    {
      "label": "report",
      "summary": "Take it from a new line in the implementer report"
    }
  ],
  "recommendation": {
    "label": "id-title",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "id-title",
  "rationale": "Owner chose '\"ID: title\" fallback (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S357"
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
