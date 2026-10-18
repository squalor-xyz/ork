---
{
  "id": 16,
  "title": "How do headless role sessions get permissions?",
  "status": "decided",
  "key": "launch.permissions",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Headless sessions cannot answer prompts.",
  "options": [
    {
      "label": "allowlist",
      "summary": "Per-project, per-role tool allowlist; read-only roles read-only"
    },
    {
      "label": "accept-edits",
      "summary": "acceptEdits plus allowlisted Bash"
    },
    {
      "label": "bypass",
      "summary": "Skip permissions"
    }
  ],
  "decision": "allowlist",
  "rationale": "Owner interview 2026-10-18: per-project allowlist per role; a denial stops the slice (contract v7).",
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
