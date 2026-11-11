---
{
  "id": 280,
  "title": "Who runs a Check line the implementer sandbox cannot run? (v2)",
  "status": "decided",
  "key": "process.orchestrator-check-lines-v2",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Owner 2026-11-11 asked why runs wait on the orchestrator; under adj 263 the orchestrator hand-ran focused tests, cross-GOOS vets and mutation steps before every approval. Replaces 263.",
  "options": [
    {
      "label": "implementer-runnable",
      "summary": "answer Check-line questions with the runnable rewrite (package-level tests instead of -run patterns, mutation steps as implementer steps); the gate verifies them; ask only if no runnable form exists or a line looks risky"
    },
    {
      "label": "orchestrator",
      "summary": "keep adj 263; the orchestrator runs them before approving"
    }
  ],
  "recommendation": {
    "label": "implementer-runnable",
    "reasoning": "Moves the work into the relay; ork's spec check recommends the runnable rewrite."
  },
  "decision": "implementer-runnable",
  "rationale": "Owner picked 'Implementer runs them (Recommended)', whose text says to supersede adj 263 with a new record.",
  "supersedes": [
    263
  ],
  "refs": [
    "S298"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    },
    {
      "date": "2026-11-11",
      "event": "supersede",
      "note": "supersedes 263"
    }
  ]
}
---
