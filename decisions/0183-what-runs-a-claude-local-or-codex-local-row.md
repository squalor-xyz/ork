---
{
  "id": 183,
  "title": "What runs a claude-local or codex-local row?",
  "status": "decided",
  "key": "roles.local-model.launcher",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "agents/llmnode/docs/agents.md:22-60 calls them shell functions; the owner says they are executables on PATH.",
  "options": [
    {
      "label": "executable",
      "summary": "an executable on PATH that ork spawns"
    },
    {
      "label": "alias",
      "summary": "a shell alias or function, so ork sets the environment itself"
    }
  ],
  "recommendation": {
    "label": "executable",
    "reasoning": "A headless spawn cannot run an alias or function."
  },
  "decision": "executable",
  "rationale": "Owner answered executable on PATH, interactive question, 2026-11-06.",
  "refs": [
    "S301"
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
