---
{
  "id": 113,
  "title": "What is a wave in ork, and where does the queue live?",
  "status": "decided",
  "key": "wave.record",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S05 needs a wave model. runrec already has a Wave field (internal/runrec/record.go:167); WaveRunning is a stub (internal/daemon/daemon.go:80).",
  "options": [
    {
      "label": "run-record-queued",
      "summary": "A wave id on run records plus a non-final queued state; caps enforced in the daemon's launchRun; queued runs restart oldest first after a daemon restart; ork run is a wave of one"
    }
  ],
  "recommendation": {
    "label": "run-record-queued",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "run-record-queued",
  "rationale": "Owner 2026-10-20: one source of truth in run.json, one admission point for caps, survives restart.",
  "refs": [
    "S05"
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
