---
{
  "id": 666,
  "title": "What flags a brief sentence gated on role when its permission grant is gated on vendor?",
  "status": "decided",
  "key": "review.role-vendor-gate-lint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S648 spec check.",
  "options": [
    {
      "label": "go-test-lint",
      "summary": "a test in internal/engine that fails when a BuildBrief sentence gated on role has a vendor-gated grant, with the sentence and grant named in the spec"
    },
    {
      "label": "unit-test",
      "summary": "unit test per sentence"
    },
    {
      "label": "runtime",
      "summary": "runtime check"
    },
    {
      "label": "drop",
      "summary": "drop"
    }
  ],
  "recommendation": {
    "label": "go-test-lint",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "go-test-lint",
  "rationale": "Owner chose 'go test lint' in an interactive question, relay session 8d711f67, 2026-11-27.",
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
