---
{
  "id": 166,
  "title": "How does ork run relays on its own repo?",
  "status": "decided",
  "key": "engine.self-host",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Retrospective R1 (docs/review-2026-10-25-relay-retrospective.md). The owner wants ork developed through ork. A slice can change the engine or daemon that runs it.",
  "options": [
    {
      "label": "main-binary-restart",
      "summary": "the daemon runs a binary built from the base branch, never a worktree; after a slice that changes internal/engine, internal/daemon or cmd lands, ork offers a daemon restart once no run is active; a wave holds at most one such slice"
    },
    {
      "label": "no-rules",
      "summary": "no self-host rules"
    }
  ],
  "recommendation": {
    "label": "main-binary-restart",
    "reasoning": "A bug in a slice under test cannot stall the run testing it, and a landed engine change takes effect at a safe point."
  },
  "decision": "main-binary-restart",
  "rationale": "Owner approved the recommendation in the agent session on 2026-10-25 (AskUserQuestion: yes, decide all 162-170); recorded by the agent at the owner's direction. ",
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    },
    {
      "date": "2026-10-25",
      "event": "decide"
    }
  ]
}
---
