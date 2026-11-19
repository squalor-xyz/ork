---
{
  "id": 487,
  "title": "How does a cloud UI pass make its renders without touching the tree?",
  "status": "superseded",
  "key": "roles.ui-pass-render-path",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "go test -overlay is already allowed (internal/engine/perm.go:23-34, adj 372).",
  "options": [
    {
      "label": "overlay",
      "summary": "Document the overlay route; no new permission"
    },
    {
      "label": "command",
      "summary": "A per-project render command"
    },
    {
      "label": "widen",
      "summary": "Widen the allowlist with cp, rsync and sed"
    }
  ],
  "recommendation": {
    "label": "overlay",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "overlay",
  "rationale": "Owner chose 'Document the overlay route (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "superseded_by": 505,
  "refs": [
    "S416"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    },
    {
      "date": "2026-11-19",
      "event": "supersede",
      "note": "by 505"
    }
  ]
}
---
