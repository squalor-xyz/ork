---
{
  "id": 371,
  "title": "Which mechanism lets a cloud UI pass write its renders, given acceptEdits auto-approves rm, mv, cp, sed, mkdir, touch and rmdir in the working directory?",
  "status": "decided",
  "key": "roles.ui-pass-write-mechanism",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S369 implementer question (run ork-20261116-151324-0200), after adj 370 chose accept-edits.",
  "options": [
    {
      "label": "plan-allow-rule",
      "summary": "keep plan mode (blocks Bash writes) and add Edit(TEMPDIR/**) for the UI pass only; the implementer checks plan mode honours it and pauses for the owner if not"
    },
    {
      "label": "accept-edits-bash-deny",
      "summary": "keep acceptEdits and add Bash deny rules for the seven commands (not a full boundary: path and sh -c forms are not matched)"
    },
    {
      "label": "accept-risk",
      "summary": "leave adj 370 as decided"
    }
  ],
  "recommendation": {
    "label": "plan-allow-rule",
    "reasoning": "Claude Code docs (permission-modes, 'Auto-approve file edits with acceptEdits mode'): acceptEdits auto-approves mkdir, touch, rm, rmdir, mv, cp and sed for paths in the working directory, which for a UI pass is the slice worktree; a Bash deny rule is not a security boundary (permissions, 'What a Bash rule doesn't match')."
  },
  "decision": "plan-allow-rule",
  "rationale": "Owner chose 'Back to plan mode (Recommended)' in an interactive question after the docs check, relay session 8d711f67, 2026-11-16.",
  "supersedes": [
    370
  ],
  "refs": [
    "S369"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    },
    {
      "date": "2026-11-16",
      "event": "supersede",
      "note": "supersedes 370"
    }
  ]
}
---
