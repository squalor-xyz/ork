---
{
  "id": 329,
  "title": "S356: which mechanism is ork's approval point for a local role?",
  "status": "decided",
  "key": "roles.local-approval-mechanism",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "options": [
    {
      "label": "hook",
      "summary": "a PreToolUse hook command served by the ork binary, passed with --settings; sees every call"
    },
    {
      "label": "prompt-tool",
      "summary": "a permission prompt tool served by ork over MCP stdio; sees only calls the allowlist does not allow"
    },
    {
      "label": "both",
      "summary": "the hook for rules, the prompt tool for asking the owner"
    }
  ],
  "recommendation": {
    "label": "hook",
    "reasoning": "Only a hook sees allowlisted Read calls, which the 500-line cap must stop; no MCP server, stdlib JSON."
  },
  "decision": "hook",
  "rationale": "Owner picked 'hook' in an interactive question on 2026-11-12 (ork relay session 8d711f67); recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
