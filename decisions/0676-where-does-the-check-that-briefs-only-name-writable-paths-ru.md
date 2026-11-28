---
{
  "id": 676,
  "title": "Where does the check that briefs only name writable paths run?",
  "status": "decided",
  "key": "brief.write-paths-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S489 spec check.",
  "options": [
    {
      "label": "test-only",
      "summary": "a test over built briefs for every role; BuildBrief unchanged"
    },
    {
      "label": "build-time",
      "summary": "BuildBrief returns an error"
    },
    {
      "label": "retire",
      "summary": "retire"
    }
  ],
  "recommendation": {
    "label": "test-only",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "test-only",
  "rationale": "Owner chose 'Test only' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S489"
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
