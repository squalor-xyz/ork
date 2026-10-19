---
{
  "id": 76,
  "title": "Where do vendor adapters live?",
  "status": "decided",
  "key": "vendor.package-path",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Go treats a vendor path element as vendored code: ./... skips it and other packages cannot import it.",
  "options": [
    {
      "label": "vendors",
      "summary": "internal/vendors/<vendor>"
    },
    {
      "label": "agent",
      "summary": "internal/agent/<vendor>"
    },
    {
      "label": "flat",
      "summary": "internal/<vendor>"
    }
  ],
  "decision": "agent",
  "rationale": "Owner 2026-10-19: internal/agent/<vendor> (claude in S16; codex and grok in S06).",
  "refs": [
    "S16"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
