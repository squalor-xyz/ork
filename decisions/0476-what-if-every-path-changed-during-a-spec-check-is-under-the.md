---
{
  "id": 476,
  "title": "What if every path changed during a spec check is under the project's .slicer/?",
  "status": "decided",
  "key": "specs.readonly-tracking-change",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Today any change is a hard error naming no path (internal/engine/speccheck.go:351-375). S292's owner answer (2026-11-04, not an adj record) kept .slicer/ in the fingerprint.",
  "options": [
    {
      "label": "retry",
      "summary": "Name the paths and treat it as an outside change (one retry)"
    },
    {
      "label": "fail",
      "summary": "Name the paths and still fail"
    },
    {
      "label": "ignore",
      "summary": "Leave the project's .slicer/ out of the fingerprint"
    }
  ],
  "recommendation": {
    "label": "retry",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "ignore",
  "rationale": "Owner chose 'Ignore .slicer/' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "narrows": [
    28
  ],
  "refs": [
    "S341"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    },
    {
      "date": "2026-11-18",
      "event": "narrow",
      "note": "narrows 28"
    }
  ]
}
---
