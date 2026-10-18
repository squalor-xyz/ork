---
{
  "id": 2,
  "title": "Does ork stay attended only, or also run unattended inside agentbox?",
  "status": "decided",
  "key": "scope.attended",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "agents ROADMAP N6 proposes an unattended cross-vendor orchestrator in agentbox; owner decision 2026-10-15 (agents/multi-agent-workflows/info/2026-10-15-nested-relay.md) allows only an owner-attended relay.",
  "options": [
    {
      "label": "attended",
      "summary": "Owner attends every run"
    },
    {
      "label": "unattended-agentbox",
      "summary": "Also allow unattended runs, only inside agentbox"
    }
  ],
  "recommendation": {
    "label": "attended",
    "reasoning": "Matches the current non-goal and the 2026-10-15 owner decision."
  },
  "decision": "attended",
  "rationale": "Owner interview 2026-10-18: approve at checkpoints: owner approves wave launch (lineup + plan) and each slice's result after the gate; roles run on their own between checkpoints. Agents run on the host.",
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
