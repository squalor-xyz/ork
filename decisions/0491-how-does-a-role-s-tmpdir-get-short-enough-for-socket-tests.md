---
{
  "id": 491,
  "title": "How does a role's TMPDIR get short enough for socket tests?",
  "status": "decided",
  "key": "roles.socket-tmpdir",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "TMPDIR is 77 bytes (internal/engine/round.go:486); unix socket paths need about 67 or less.",
  "options": [
    {
      "label": "state-root",
      "summary": "A short per-round TMPDIR under the state root, 0700, removed with the run"
    },
    {
      "label": "short-names",
      "summary": "Shorter dir names under the run dir"
    },
    {
      "label": "tmp",
      "summary": "Test helpers fall back to /tmp"
    }
  ],
  "recommendation": {
    "label": "state-root",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "state-root",
  "rationale": "Owner chose 'Short TMPDIR under state root (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S437"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
