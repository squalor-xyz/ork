---
{
  "id": 495,
  "title": "How far does S437's short TMPDIR change go?",
  "status": "decided",
  "key": "roles.socket-tmpdir-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "adj 491 puts a short per-round TMPDIR under the state root; the implementer asked whether the role's temp folder moves too (run ork-20261118-225522-3de5).",
  "options": [
    {
      "label": "separate",
      "summary": "Keep the role temp folder; add a separate short TMPDIR under the state root with cleanup and tests; no /tmp fallback"
    },
    {
      "label": "move",
      "summary": "Move the whole role temp folder to the short path"
    },
    {
      "label": "fallback",
      "summary": "Separate TMPDIR plus a fallback outside the state root"
    }
  ],
  "recommendation": {
    "label": "separate",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "separate",
  "rationale": "Owner chose 'Separate short TMPDIR (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
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
