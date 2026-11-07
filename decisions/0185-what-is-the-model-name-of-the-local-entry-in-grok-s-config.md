---
{
  "id": 185,
  "title": "What is the model name of the local entry in Grok's config?",
  "status": "decided",
  "key": "roles.local-model.grok-model",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "ork needs the name to tell a local Grok row from a cloud one.",
  "recommendation": {
    "text": "The owner gives the name from ~/.grok/config.toml; the sibling docs say studio-local but describe the file without reading it."
  },
  "decision": "The Grok config entry is [model.studio-local] with model = \"qwen3.8-27b\" in ~/.grok/config.toml (owner, typed 2026-11-06). A Grok row selects it with -m studio-local.",
  "rationale": "Owner typed the entry in an interactive question, 2026-11-06.",
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
