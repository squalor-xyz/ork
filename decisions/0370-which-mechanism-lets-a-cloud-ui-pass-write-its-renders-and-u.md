---
{
  "id": 370,
  "title": "Which mechanism lets a cloud UI pass write its renders, and under which path?",
  "status": "superseded",
  "key": "roles.ui-pass-write-mechanism",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S369 spec check question.",
  "options": [
    {
      "label": "allow-rule",
      "summary": "keep plan mode and add --allowedTools Edit(TEMPDIR/**) for the UI role only"
    },
    {
      "label": "accept-edits",
      "summary": "switch the UI pass to --permission-mode acceptEdits with path rules that deny Edit/Write outside TEMPDIR, so the worktree stays unwritable"
    },
    {
      "label": "add-dir",
      "summary": "keep plan mode and pass --add-dir TEMPDIR"
    }
  ],
  "recommendation": {
    "label": "allow-rule",
    "reasoning": "smallest change; keeps plan mode's read-only guard on the worktree"
  },
  "decision": "accept-edits",
  "rationale": "Owner chose 'Accept edits' (not the recommendation) in an interactive question, relay session 8d711f67, 2026-11-16.",
  "superseded_by": 371,
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
      "note": "by 371"
    }
  ]
}
---
