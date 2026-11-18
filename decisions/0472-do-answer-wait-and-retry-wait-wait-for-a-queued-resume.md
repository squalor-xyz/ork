---
{
  "id": 472,
  "title": "Do answer --wait and retry --wait wait for a queued resume?",
  "status": "decided",
  "key": "cli.wait-queued-resume",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "The stored-resume path returns before the NoWait check (internal/daemon/runs.go:2433-2458).",
  "options": [
    {
      "label": "wait",
      "summary": "Wait until the resume starts and the run rests again"
    },
    {
      "label": "return",
      "summary": "Return at once, as today, and say so in the help"
    }
  ],
  "recommendation": {
    "label": "wait",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "wait",
  "rationale": "Owner chose 'Wait until it rests (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S307"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
