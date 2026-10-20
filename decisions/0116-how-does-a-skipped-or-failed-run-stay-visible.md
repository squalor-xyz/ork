---
{
  "id": 116,
  "title": "How does a skipped or failed run stay visible?",
  "status": "decided",
  "key": "run.background-errors",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "recoverRuns skips on a reconcile error (internal/daemon/runs.go:111-137) and background errors are not written to the record.",
  "options": [
    {
      "label": "memory-error",
      "summary": "The run stays in the run manager with an error held in daemon memory on the wire row; no new final state; retry and stop keep working"
    }
  ],
  "recommendation": {
    "label": "memory-error",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "memory-error",
  "rationale": "Owner 2026-10-20: a final state is never reconciled again, so a failed kill could leave an orphaned process group.",
  "refs": [
    "S35"
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
