---
{
  "id": 622,
  "title": "What bounds the go test runs ork makes while building the Base overlays section?",
  "status": "decided",
  "key": "review.overlay-test-bound",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S622 spec draft.",
  "options": [
    {
      "label": "per-run-2min",
      "summary": "each overlay's go test gets a 2-minute timeout like vet; sequential; a run past it leaves the entry plain"
    },
    {
      "label": "budget",
      "summary": "plus a 5-minute section budget"
    },
    {
      "label": "5min",
      "summary": "5 minutes per run"
    }
  ],
  "recommendation": {
    "label": "per-run-2min",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "per-run-2min",
  "rationale": "Owner chose '2 min each only' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S622"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
