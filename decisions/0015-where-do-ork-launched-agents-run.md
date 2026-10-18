---
{
  "id": 15,
  "title": "Where do ork-launched agents run?",
  "status": "decided",
  "key": "launch.sandbox",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "agents repo allowed unattended runs only in agentbox; ork runs are attended at checkpoints.",
  "options": [
    {
      "label": "host",
      "summary": "On the host, in the slice worktree, with scoped permissions"
    },
    {
      "label": "agentbox",
      "summary": "Always inside agentbox"
    },
    {
      "label": "per-project",
      "summary": "Per-project setting"
    }
  ],
  "decision": "host",
  "rationale": "Owner interview 2026-10-18: host, scoped permissions; agentbox optional later.",
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
