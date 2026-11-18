---
{
  "id": 489,
  "title": "How do read-only Claude roles avoid the plan-mode instruction conflict?",
  "status": "decided",
  "key": "roles.read-only-plan-mode",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "internal/agent/claude/args.go:103-105, internal/engine/brief.go:249-255; runs ork-20261117-233556-f24d, ork-20261118-121813-4c84.",
  "options": [
    {
      "label": "brief",
      "summary": "Keep plan mode; the brief says not to exit plan mode and to report in the final message"
    },
    {
      "label": "allowlist",
      "summary": "Allowlist mode on Claude (as adj 372 did for the UI pass); plan mode stays on Grok"
    },
    {
      "label": "leave",
      "summary": "Leave plan mode on both"
    }
  ],
  "recommendation": {
    "label": "brief",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "allowlist",
  "rationale": "Owner chose 'Allowlist mode on Claude' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S418"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
