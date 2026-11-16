---
{
  "id": 372,
  "title": "How does a cloud UI pass write its renders, given plan mode refuses writes even with an Edit allow rule?",
  "status": "decided",
  "key": "roles.ui-pass-write-mechanism",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S369 implementer question (run ork-20261116-151324-0200); adj 371's mechanism failed the probe.",
  "options": [
    {
      "label": "allowlist-mode",
      "summary": "run the UI pass with a read-only allowlist (Read, Grep, Glob, read-only Bash rules) plus Edit(TEMPDIR/**) and --permission-prompts none; every other call is denied"
    },
    {
      "label": "no-renders",
      "summary": "keep plan mode; renders go inline in the report"
    }
  ],
  "recommendation": {
    "label": "allowlist-mode",
    "reasoning": "probe 2026-11-16 (claude 2.1, haiku, .scratch/plan-probe): plan mode returned 'Cannot write to ... while in plan mode' for a Write under an Edit(TEMPDIR/**) allow rule with --add-dir; allowlist mode does not auto-approve rm, mv or sed as acceptEdits does"
  },
  "decision": "allowlist-mode",
  "rationale": "Owner chose 'Allowlist mode (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "supersedes": [
    371
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
      "note": "supersedes 371"
    }
  ]
}
---
