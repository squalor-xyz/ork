---
{
  "id": 166,
  "title": "How does ork run relays on its own repo?",
  "status": "open",
  "key": "engine.self-host",
  "asked_by": "claude",
  "created": "2026-10-25",
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
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    }
  ]
}
---
