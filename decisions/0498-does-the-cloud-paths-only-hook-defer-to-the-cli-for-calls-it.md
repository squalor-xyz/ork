---
{
  "id": 498,
  "title": "Does the cloud paths-only hook defer to the CLI for calls it does not deny?",
  "status": "decided",
  "key": "launch.cloud-hook-passthrough",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "With adj 497 the hook denied every call outside the allowlist, including permission-free tools such as TodoWrite and Task (S345 reviewer, run ork-20261118-225932-056b).",
  "options": [
    {
      "label": "defer",
      "summary": "Return no decision for calls the hook does not deny; the CLI's allowlist and deny rules decide"
    },
    {
      "label": "keep-deny",
      "summary": "Deny every call outside the allowlist"
    },
    {
      "label": "exempt-list",
      "summary": "Keep deny and add permission-free tools to the allowlist"
    }
  ],
  "recommendation": {
    "label": "defer",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "defer",
  "rationale": "Owner chose 'Defer to the CLI (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S345"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
