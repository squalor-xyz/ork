---
{
  "id": 602,
  "title": "What should harnesses including the run's own tracking paths by default change?",
  "status": "decided",
  "key": "tests.diff-harness-tracking-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S616 tool feedback from S584's reviewer.",
  "options": [
    {
      "label": "shared-default",
      "summary": "a shared diff-route helper prepends .slicer/ and landed-changed/ paths to every routed git diff --name-only; tests can opt out"
    },
    {
      "label": "drop",
      "summary": "close S616"
    },
    {
      "label": "one-harness",
      "summary": "only noRendersHarness"
    }
  ],
  "recommendation": {
    "label": "shared-default",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "shared-default",
  "rationale": "Owner chose 'Shared default' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S616"
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
