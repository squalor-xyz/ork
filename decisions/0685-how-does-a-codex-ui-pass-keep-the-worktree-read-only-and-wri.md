---
{
  "id": 685,
  "title": "How does a codex UI pass keep the worktree read-only and write only the renders directory?",
  "status": "decided",
  "key": "ui.renders-dir-grant-codex",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S667 spec draft.",
  "options": [
    {
      "label": "probe-first",
      "summary": "the implementer probes installed codex for a read-only worktree with one writable root, saves the probe under testdata/vendors/codex/, builds on it; if none exists it stops and reports"
    },
    {
      "label": "defer-codex",
      "summary": "defer codex"
    }
  ],
  "recommendation": {
    "label": "probe-first",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "probe-first",
  "rationale": "Owner chose 'Probe first, stop if none' in an interactive question, relay session 8d711f67, 2026-11-27.",
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
