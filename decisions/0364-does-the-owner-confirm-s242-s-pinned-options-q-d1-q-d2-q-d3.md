---
{
  "id": 364,
  "title": "Does the owner confirm S242's pinned options Q-D1, Q-D2, Q-D3 and Q-D5?",
  "status": "decided",
  "key": "daemon.stale-rule",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S242 spec check question.",
  "options": [
    {
      "label": "confirm-all",
      "summary": "Q-D1 b stale only when files the binary is built from differ; Q-D2 a warnings unchanged; Q-D3 a doctor and daemon status show behind-main while roles run; Q-D5 a daemon status exits 0 when no daemon runs"
    },
    {
      "label": "q-d1-only",
      "summary": "record the stale rule; the other three stay proposed"
    },
    {
      "label": "change",
      "summary": "the owner states a different option"
    }
  ],
  "recommendation": {
    "label": "confirm-all",
    "reasoning": "the spec pins the recommended options and its tests are complete for them"
  },
  "decision": "confirm-all",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-16 ('Confirm all (Recommended)').",
  "refs": [
    "S242"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
