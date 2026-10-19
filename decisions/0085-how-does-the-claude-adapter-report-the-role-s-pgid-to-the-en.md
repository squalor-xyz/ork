---
{
  "id": 85,
  "title": "How does the claude adapter report the role's pgid to the engine?",
  "status": "decided",
  "key": "engine.adapter-start",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "engine.Request.OnStart(pgid) is required (internal/engine/runner.go:45); claude.Run blocks on cmd.Run() (internal/agent/claude/run.go:63) and claude.Spec has no start hook.",
  "options": [
    {
      "label": "callback",
      "summary": "add Spec.OnStart func(pgid int), called after cmd.Start and before cmd.Wait"
    },
    {
      "label": "handle",
      "summary": "split Run into Start and Wait returning a handle"
    }
  ],
  "decision": "callback",
  "rationale": "Owner 2026-10-19: add Spec.OnStart func(pgid int) to claude.Spec; Run calls it after a successful cmd.Start and before cmd.Wait; the glue forwards the engine callback.",
  "refs": [
    "S17"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
