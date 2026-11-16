---
{
  "id": 368,
  "title": "Where does the full permission command live on the wire?",
  "status": "decided",
  "key": "approval.question-detail-field",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S370 spec check question.",
  "options": [
    {
      "label": "question-field",
      "summary": "add an optional detail field to runrec.OwnerQuestion; ork status --json and ork show print it in full; the title keeps a preview of at most 120 characters"
    },
    {
      "label": "separate-fields",
      "summary": "a permission-detail field on EngineRun and a new field in ShowReply or ShowPending, filled only from the live wait"
    }
  ],
  "recommendation": {
    "label": "question-field",
    "reasoning": "one field on the type status --json already carries"
  },
  "decision": "question-field",
  "rationale": "Owner chose 'Question field (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S370"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
