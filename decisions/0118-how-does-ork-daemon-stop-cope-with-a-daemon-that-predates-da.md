---
{
  "id": 118,
  "title": "How does ork daemon stop cope with a daemon that predates daemon.stop?",
  "status": "decided",
  "key": "daemon.legacy-stop",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "SIGTERM cancels the daemon context (cmd/ork/main.go:65) and kills running roles, the same as --force.",
  "options": [
    {
      "label": "print-pid",
      "summary": "Print the pid and kill -TERM PID, and warn if a role is running; ork does not signal the pid itself"
    }
  ],
  "recommendation": {
    "label": "print-pid",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "print-pid",
  "rationale": "Owner 2026-10-20: a one-time upgrade issue, and no risk of hitting a reused pid.",
  "refs": [
    "S56"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
