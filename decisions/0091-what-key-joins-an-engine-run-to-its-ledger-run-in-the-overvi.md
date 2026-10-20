---
{
  "id": 91,
  "title": "What key joins an engine run to its ledger run in the Overview?",
  "status": "decided",
  "key": "ui.run-join-key",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "adj ui.run-join says slice and worktree; the engine record links its ledger run (runrec.Record.RelayRun, internal/runrec/record.go) and the wire EngineRun has no worktree.",
  "options": [
    {
      "label": "run-link",
      "summary": "match RelayRun to the ledger run id; refines ui.run-join, not a reversal"
    },
    {
      "label": "worktree",
      "summary": "add worktree to EngineRun and compare cleaned paths"
    }
  ],
  "decision": "run-link",
  "rationale": "Owner 2026-10-20: join on the engine record's relay-run link to the ledger run id. This refines the join rule in ui.run-join (still one row per run).",
  "refs": [
    "S22"
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
