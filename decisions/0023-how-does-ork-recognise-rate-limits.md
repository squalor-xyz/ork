---
{
  "id": 23,
  "title": "How does ork recognise rate limits?",
  "status": "decided",
  "key": "vendor.ratelimit-detection",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "No captured fixtures exist.",
  "options": [
    {
      "label": "capture-conservative",
      "summary": "Capture on failure, conservative rules"
    },
    {
      "label": "trigger",
      "summary": "Deliberately trigger limits before v1"
    }
  ],
  "decision": "capture-conservative",
  "rationale": "Owner interview 2026-10-18: capture raw output of every non-ok exit as a fixture candidate; classify only documented signals; anything unclassified stops the slice for the owner.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
