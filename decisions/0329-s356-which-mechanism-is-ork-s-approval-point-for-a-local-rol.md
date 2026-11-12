---
{
  "id": 329,
  "title": "S356: which mechanism is ork's approval point for a local role?",
  "status": "open",
  "key": "roles.local-approval-mechanism",
  "asked_by": "claude",
  "created": "2026-11-12",
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
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    }
  ]
}
---
