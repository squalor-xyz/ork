---
{
  "id": 71,
  "title": "Where does ork record the writes it makes for a run?",
  "status": "decided",
  "key": "state.writes",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Integration review R7: after a crash or a locked retry, the owner and the restart reconcile need to know what ork changed in slicer, adj and git.",
  "options": [
    {
      "label": "run-json",
      "summary": "A writes[] list in run.json"
    },
    {
      "label": "separate-log",
      "summary": "A separate writes.log per run"
    },
    {
      "label": "none",
      "summary": "Do not record them"
    }
  ],
  "decision": "run-json",
  "rationale": "Owner 2026-10-19: writes[] in run.json.",
  "refs": [
    "S04"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
