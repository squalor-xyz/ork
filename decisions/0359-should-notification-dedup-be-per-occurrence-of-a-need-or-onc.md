---
{
  "id": 359,
  "title": "Should notification dedup be per occurrence of a need, or once per event per run?",
  "status": "decided",
  "key": "notify.dedup-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S311 question (run ork-20261115-033114-333e): Run.ClaimNotification (internal/runrec/store.go:278) keys on the event name only.",
  "options": [
    {
      "label": "per-occurrence",
      "summary": "key the claim on the event plus the transition time, so each new need notifies once, including across a restart"
    },
    {
      "label": "per-run",
      "summary": "one notification per event per run"
    }
  ],
  "recommendation": {
    "label": "per-occurrence",
    "reasoning": "the purpose is to tell the owner each time a run needs them"
  },
  "decision": "per-occurrence",
  "rationale": "Owner chose 'Per occurrence (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-15.",
  "refs": [
    "S311"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
