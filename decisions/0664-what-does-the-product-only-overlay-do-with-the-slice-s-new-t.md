---
{
  "id": 664,
  "title": "What does the product-only overlay do with the slice's new test files?",
  "status": "decided",
  "key": "base-overlays.product-only-overlay",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S643 spec check (adj 655).",
  "options": [
    {
      "label": "keep-tests",
      "summary": "revert only the product file and keep the new tests; tests calling new symbols fail to build"
    },
    {
      "label": "stub-symbols",
      "summary": "generate symbol stubs"
    },
    {
      "label": "empty-stub",
      "summary": "empty test stub"
    }
  ],
  "recommendation": {
    "label": "keep-tests",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "keep-tests",
  "rationale": "Owner chose 'Keep tests, compile red' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S643"
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
