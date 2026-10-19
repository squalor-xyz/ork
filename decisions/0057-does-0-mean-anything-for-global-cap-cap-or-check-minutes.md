---
{
  "id": 57,
  "title": "Does 0 mean anything for global_cap, cap or check_minutes?",
  "status": "decided",
  "key": "registry.zero-values",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "S02 Save omits zero values, so they read back as defaults.",
  "options": [
    {
      "label": "unset",
      "summary": "0 means unset, default applies"
    },
    {
      "label": "pause",
      "summary": "cap 0 pauses the project; keep explicit zeros"
    }
  ],
  "decision": "unset",
  "rationale": "Owner 2026-10-19: 0 is the same as absent and the default applies; pausing a project is a UI action, not a cap.",
  "refs": [
    "S02"
  ],
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
