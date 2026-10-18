---
{
  "id": 35,
  "title": "Where do slice worktrees go?",
  "status": "decided",
  "key": "git.worktrees",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Projects use .worktrees/sNN on branch sNN.",
  "options": [
    {
      "label": "per-project-pattern",
      "summary": "Per-project pattern"
    },
    {
      "label": "ork-location",
      "summary": "ork-owned location"
    }
  ],
  "decision": "per-project-pattern",
  "rationale": "Owner interview 2026-10-18: registry worktree_pattern (default .worktrees/{id}) and branch_pattern (default {id}).",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
