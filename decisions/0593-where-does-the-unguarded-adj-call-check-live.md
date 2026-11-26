---
{
  "id": 593,
  "title": "Where does the unguarded-adj-call check live?",
  "status": "decided",
  "key": "engine.adj-guard-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S570 spec check.",
  "options": [
    {
      "label": "go-test",
      "summary": "go/parser test in internal/engine with an explicit allowlist of unguarded sites"
    },
    {
      "label": "reviewer-rule",
      "summary": "reviewer prompt only"
    },
    {
      "label": "slicer",
      "summary": "slicer lint"
    }
  ],
  "recommendation": {
    "label": "go-test",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "go-test",
  "rationale": "Owner chose 'Go test' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S570"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
