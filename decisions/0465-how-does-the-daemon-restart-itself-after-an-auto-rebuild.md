---
{
  "id": 465,
  "title": "How does the daemon restart itself after an auto-rebuild?",
  "status": "decided",
  "key": "selfhost.auto-rebuild-restart",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S431 spec check (2026-11-18): the restart path is driven by the CLI today (cmd/ork/start.go:48-90).",
  "options": [
    {
      "label": "successor",
      "summary": "before exiting, the daemon starts ork daemon run from the new executable, detached, with the CLI's validation and start code moved to a shared place; the new daemon takes the lock and recovers runs"
    },
    {
      "label": "exit-only",
      "summary": "the daemon validates the new executable and stops; the next client starts it"
    },
    {
      "label": "exec",
      "summary": "syscall.Exec the new binary at its own path, redoing socket and lock handling"
    }
  ],
  "recommendation": {
    "label": "successor",
    "reasoning": "Ends on the new build with no hand command; exec changes lock and socket handling beyond the spec."
  },
  "decision": "successor",
  "rationale": "Owner chose 'Start successor, then exit (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S431"
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
