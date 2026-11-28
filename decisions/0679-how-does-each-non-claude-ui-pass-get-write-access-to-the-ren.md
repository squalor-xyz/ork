---
{
  "id": 679,
  "title": "How does each non-claude UI pass get write access to the renders directory?",
  "status": "decided",
  "key": "ui.renders-dir-grant-mechanism",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S667 spec check.",
  "options": [
    {
      "label": "narrow-per-vendor",
      "summary": "codex gets the renders directory as its only writable root with the worktree read-only; grok and claude-local get the same single-path write rule through their own permission mechanisms"
    },
    {
      "label": "writable-guard",
      "summary": "writable mode plus guard"
    },
    {
      "label": "defer",
      "summary": "claude-local now, defer codex and grok"
    }
  ],
  "recommendation": {
    "label": "narrow-per-vendor",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "narrow-per-vendor",
  "rationale": "Owner chose 'Narrow per-vendor grant' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S667"
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
