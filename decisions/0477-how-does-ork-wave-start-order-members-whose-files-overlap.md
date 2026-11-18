---
{
  "id": 477,
  "title": "How does ork wave start order members whose Files overlap?",
  "status": "decided",
  "key": "wave.overlap-order",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "The daemon refuses only depends_on blockers and engine-slice pairs (internal/daemon/runs.go:1648-1738).",
  "options": [
    {
      "label": "hold",
      "summary": "A later member in the typed order waits until the earlier overlapping one lands (slicer deps --overlap)"
    },
    {
      "label": "warn",
      "summary": "Warn and confirm; --yes starts all"
    },
    {
      "label": "refuse",
      "summary": "Refuse the wave and name the pairs"
    }
  ],
  "recommendation": {
    "label": "hold",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "hold",
  "rationale": "Owner chose 'Hold behind (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S342"
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
