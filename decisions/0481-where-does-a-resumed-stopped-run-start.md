---
{
  "id": 481,
  "title": "Where does a resumed stopped run start?",
  "status": "decided",
  "key": "run.resume-open-findings",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "internal/engine/resume.go:139-160; S296 started it at the checks.",
  "options": [
    {
      "label": "implementer",
      "summary": "An implementer round carrying the stored findings, or the checks when they came from a red check"
    },
    {
      "label": "checks",
      "summary": "At the checks, as S296; fix the help text only"
    }
  ],
  "recommendation": {
    "label": "implementer",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "implementer",
  "rationale": "Owner chose 'Implementer with findings (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S349"
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
