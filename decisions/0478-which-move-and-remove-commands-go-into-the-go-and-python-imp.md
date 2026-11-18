---
{
  "id": 478,
  "title": "Which move and remove commands go into the go and python implementer presets?",
  "status": "decided",
  "key": "launch.allowlist-move",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S345 asks for moves and S440 for removals; both change ImplementerPreset (internal/engine/perm.go:45-70). The slice commit stages with git add -A.",
  "options": [
    {
      "label": "all",
      "summary": "git ls-files and git mv (plain and -C), git rm, and plain rm; recursive rm stays denied by the hook; merge S345 and S440"
    },
    {
      "label": "git",
      "summary": "git ls-files, git mv and git rm only"
    },
    {
      "label": "none",
      "summary": "git ls-files only; the orchestrator moves and removes files"
    }
  ],
  "recommendation": {
    "label": "all",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "all",
  "rationale": "Owner chose 'ls-files, mv, git rm, rm (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S345"
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
