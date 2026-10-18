---
{
  "id": 12,
  "title": "Which vendor and harness does ork launch the orchestrator on, and how does it detect completion?",
  "status": "decided",
  "key": "launch.vendor",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Today the orchestrator is a Claude subagent that spawns role subagents (depth 0-2, slice-relay.md). Launched headless by ork it would be depth 0 itself. Headless invocations are in agents/setup/lib/workflow-vendor.sh.",
  "options": [
    {
      "label": "claude-headless",
      "summary": "claude -p with JSON output in the slice worktree; completion is process exit plus the run report"
    },
    {
      "label": "any-vendor",
      "summary": "Any vendor from the lineup via the agents repo launcher"
    }
  ],
  "recommendation": {
    "label": "claude-headless",
    "reasoning": "Only Claude has the orchestrator agent type and nested subagents today."
  },
  "decision": "All three vendors (claude, codex, grok) in v1. ork launches each role headlessly; completion is process exit plus a report file.",
  "rationale": "Owner interview 2026-10-18: all three vendors; ork launches every role; process exit + report file.",
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
