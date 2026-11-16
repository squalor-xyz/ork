---
{
  "id": 379,
  "title": "How do runs use Grok as implementer until the grok accounts end on 2026-12-09?",
  "status": "decided",
  "key": "roles.grok-implementer-row",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S305 landed 2026-11-16; ork's matrix had no grok implementer row.",
  "options": [
    {
      "label": "matrix-first",
      "summary": "add a Grok implementer row (whole slice, latest, medium) ahead of codex; codex then claude sonnet fall back; S374 removes it before 2026-12-09"
    },
    {
      "label": "per-run",
      "summary": "no matrix change; pass --implementer grok first on each run"
    },
    {
      "label": "after-codex",
      "summary": "a Grok row behind codex"
    }
  ],
  "recommendation": {
    "label": "matrix-first",
    "reasoning": "the owner wants grok used fully until 2026-12-09; codex is often out of credits"
  },
  "decision": "matrix-first",
  "rationale": "Owner chose 'Matrix row, first (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S305",
    "S374"
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
