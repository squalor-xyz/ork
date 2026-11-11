---
{
  "id": 277,
  "title": "S287: how does the keep-your-question flag reach the shape retry brief?",
  "status": "decided",
  "key": "shape.retry-asked-flag",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S287 spec check 2026-11-11: pendingSection and pendingKindSection (internal/engine/round.go:140, :151) see only *runrec.Pending, not the refused report.",
  "options": [
    {
      "label": "pending-field",
      "summary": "add Asked bool (omitempty) to runrec.Pending, set in the shape branch of runRound, read by pendingKindSection next to AutomaticShapeRetry"
    },
    {
      "label": "read-report",
      "summary": "no new field; the brief builder re-reads the refused report and derives asked"
    },
    {
      "label": "fold-into-reason",
      "summary": "no new field; append the sentence to p.Reason"
    }
  ],
  "recommendation": {
    "label": "pending-field",
    "reasoning": "Follows the AutomaticShapeRetry pattern, cheap and testable, keeps Reason a plain problem list."
  },
  "decision": "pending-field",
  "rationale": "Owner picked Pending field (recommended) in an interactive question on 2026-11-11.",
  "refs": [
    "S287"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
