---
{
  "id": 360,
  "title": "How does a run's lineup override reach the spec check over the wire?",
  "status": "decided",
  "key": "speccheck.lineup-override-wire",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S366 spec check question.",
  "options": [
    {
      "label": "lineups-field",
      "summary": "optional per-item lineup map on SpecCheckParams (RunParams.Lineup shape); Items unchanged; ork run and wave start fill it"
    },
    {
      "label": "runs-field",
      "summary": "add Runs []RunParams to SpecCheckParams"
    },
    {
      "label": "replace-items",
      "summary": "change Items to []RunParams (breaking)"
    }
  ],
  "recommendation": {
    "label": "lineups-field",
    "reasoning": "additive; carries only what IsLocalRun needs; poller and TUI checks unchanged"
  },
  "decision": "lineups-field",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-15 ('Lineups field (Recommended)').",
  "refs": [
    "S366"
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
