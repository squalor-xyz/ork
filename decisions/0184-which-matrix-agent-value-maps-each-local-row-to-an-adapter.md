---
{
  "id": 184,
  "title": "Which matrix Agent value maps each local row to an adapter?",
  "status": "decided",
  "key": "roles.local-model.agent-value",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "Row r33 uses Local (vLLM), which names a server, not a harness.",
  "options": [
    {
      "label": "launcher-names",
      "summary": "claude-local and codex-local, and Agent Grok with the local model"
    },
    {
      "label": "local-vllm",
      "summary": "one Agent value Local (vLLM) for all"
    }
  ],
  "recommendation": {
    "label": "launcher-names",
    "reasoning": "Keeps adj 155: the Agent field names the harness."
  },
  "decision": "launcher-names",
  "rationale": "Owner picked launcher names, interactive question, 2026-11-06.",
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
