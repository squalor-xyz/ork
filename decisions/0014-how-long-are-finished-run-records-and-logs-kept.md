---
{
  "id": 14,
  "title": "How long are finished run records and logs kept?",
  "status": "decided",
  "key": "state.runs-retention",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Run records and orchestrator logs accumulate under ~/.local/state/ork/runs (docs/state.md).",
  "options": [
    {
      "label": "forever",
      "summary": "Keep all; owner prunes"
    },
    {
      "label": "days-30",
      "summary": "Prune finished runs older than 30 days"
    },
    {
      "label": "count-100",
      "summary": "Keep the latest 100 finished runs"
    }
  ],
  "decision": "count-100",
  "rationale": "Owner 2026-10-18: keep the latest 100 finished runs; a run whose branch is not landed is never pruned.",
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
