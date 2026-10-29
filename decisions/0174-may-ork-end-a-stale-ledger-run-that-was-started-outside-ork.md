---
{
  "id": 174,
  "title": "May ork end a stale ledger run that was started outside ork?",
  "status": "decided",
  "key": "dash.external-runs-end",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-29",
  "decided_at": "2026-10-29",
  "context": "S175 (end a stale ledger run from the run pane) acts on runs started elsewhere. Decided adj 42 (dash.external-runs) says view-only, ork never acts on them; docs/boundaries.md:77 says the same. Owner parity decision 2026-10-29 listed gap 10 (end a stale ledger run) as a v1.0 slice. Refs S175.",
  "options": [
    {
      "label": "end-stale",
      "summary": "ork may end a ledger run that slicer reports as stale (no heartbeat for 30 minutes) after a y/N confirm, with slicer relay-run end RUN --outcome stopped --close-steps fail; it never acts on an active or ended external run",
      "tradeoffs": "Closes the recovery gap (S175) so the owner needs no outer session to clear stale rows; amends adj 42 for stale runs only; ork refuses when an unfinished ork engine run owns the ledger run"
    },
    {
      "label": "view-only",
      "summary": "ork stays view-only for relays started elsewhere (adj 42); the owner ends stale runs with slicer in a terminal",
      "tradeoffs": "Keeps adj 42 and docs/boundaries.md:77 as written; stale external rows stay until ended with slicer"
    }
  ],
  "recommendation": {
    "label": "end-stale",
    "reasoning": "Owner direction 2026-10-29: never need an orchestrator session; stale rows are the one external state that needs cleanup and ending them is a confirmed, advisory ledger write."
  },
  "decision": "end-stale",
  "rationale": "Owner picked end-stale in an interactive question in the ork orchestration session, 2026-10-29 (S175 review). adj 42 (dash.external-runs, view-only) still needs the owner's supersede.",
  "refs": [
    "S175",
    "42"
  ],
  "history": [
    {
      "date": "2026-10-29",
      "event": "ask"
    },
    {
      "date": "2026-10-29",
      "event": "decide"
    }
  ]
}
---
