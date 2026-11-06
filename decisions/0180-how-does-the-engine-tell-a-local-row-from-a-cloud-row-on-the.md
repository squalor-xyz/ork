---
{
  "id": 180,
  "title": "How does the engine tell a local row from a cloud row on the same CLI?",
  "status": "decided",
  "key": "roles.local-model.row-identity",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "Env, vendor-out marks and slot caps key on claude, codex or grok (internal/daemon/runs.go:187-203, runner.go:90, vendors.go:15).",
  "options": [
    {
      "label": "own-vendor",
      "summary": "distinct lineup vendor with its own env, out mark and cap"
    },
    {
      "label": "agent-per-harness",
      "summary": "new Agent value per harness"
    },
    {
      "label": "model-name",
      "summary": "detect by model name"
    }
  ],
  "recommendation": {
    "label": "own-vendor",
    "reasoning": "A cloud rate limit must not take out a local row, and local needs its own cap."
  },
  "decision": "Local rows get their own lineup vendor (claude-local, codex-local, grok-local) with their own env, out mark and cap. Owner picked detection by model name, then said it would be claude-local and codex-local, and confirmed a separate identity for marks and caps.",
  "rationale": "Owner answers, two interactive questions, 2026-11-06.",
  "refs": [
    "S301",
    "adj:155"
  ],
  "history": [
    {
      "date": "2026-11-06",
      "event": "ask"
    },
    {
      "date": "2026-11-06",
      "event": "decide"
    }
  ]
}
---
