---
{
  "id": 8,
  "title": "How far does ork go in launching agent work?",
  "status": "decided",
  "key": "launch.mode",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Owner answer 2026-10-18. Supersedes the 2026-10-15 'no external loop script' stance for ork, within an owner-attended run.",
  "options": [
    {
      "label": "launch-supervise",
      "summary": "ork runs Setup, launches the orchestrator headless and supervises it; owner approves each launch, gate result and landing"
    },
    {
      "label": "setup-handoff",
      "summary": "ork prepares the worktree, relay-run and brief; a session the owner starts runs it"
    },
    {
      "label": "scheduler",
      "summary": "ork runs waves on its own, unattended only inside agentbox"
    }
  ],
  "recommendation": {
    "label": "launch-supervise",
    "reasoning": "Owner's 2026-10-18 answer."
  },
  "decision": "ork is the state machine: it runs Setup, launches every role process itself (no orchestrator agent), and supervises; the owner approves at checkpoints.",
  "rationale": "Owner interview 2026-10-18: ork launches every role and passes reports between them; the headless-orchestrator experiment is dropped.",
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
