---
{
  "id": 618,
  "title": "How does ork say a base overlay passes the whole package, and that a _test.go-only change cannot be isolated?",
  "status": "decided",
  "key": "reviewer.overlay-pass-note",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S622 spec check.",
  "options": [
    {
      "label": "run-at-brief",
      "summary": "ork runs each overlay's go test while building the section (bounded, advisory) and marks files whose package passes in full; says when every changed Go file is a _test.go file"
    },
    {
      "label": "fixed-text",
      "summary": "fixed sentences only"
    }
  ],
  "recommendation": {
    "label": "run-at-brief",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "run-at-brief",
  "rationale": "Owner chose 'Run at brief' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S622"
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
