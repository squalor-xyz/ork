---
{
  "id": 671,
  "title": "Fix the UI renders sentence the lint flags, or exempt it?",
  "status": "decided",
  "key": "review.brief-grant-vendor-lint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S648 spec draft.",
  "options": [
    {
      "label": "gate-on-vendor",
      "summary": "gate the UI renders directory sentence (brief.go:231-232) on in.Vendor == claude"
    },
    {
      "label": "exempt",
      "summary": "exempt it"
    }
  ],
  "recommendation": {
    "label": "gate-on-vendor",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "gate-on-vendor",
  "rationale": "Owner chose 'Gate on vendor' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S648"
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
