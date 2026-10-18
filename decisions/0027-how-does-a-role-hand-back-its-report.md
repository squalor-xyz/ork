---
{
  "id": 27,
  "title": "How does a role hand back its report?",
  "status": "decided",
  "key": "role.report-io",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Hand-backs were misrouted in Claude Code relays.",
  "options": [
    {
      "label": "exit-and-file",
      "summary": "Process exit plus report file"
    },
    {
      "label": "json",
      "summary": "Vendor JSON result only"
    }
  ],
  "decision": "exit-and-file",
  "rationale": "Owner interview 2026-10-18: role writes its report to a path in ork state and exits; fallback to the vendor JSON result.",
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
