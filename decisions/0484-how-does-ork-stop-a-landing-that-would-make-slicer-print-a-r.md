---
{
  "id": 484,
  "title": "How does ork stop a landing that would make slicer print a relay version ork does not accept?",
  "status": "decided",
  "key": "land.contract-bump-guard",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "The installed slicer imports main's source; there is no check before the fast-forward (internal/engine/land.go:95-97).",
  "options": [
    {
      "label": "probe",
      "summary": "projects set --contract-probe CMD, run in the worktree before merge --ff-only; pause on an unaccepted version"
    },
    {
      "label": "guard",
      "summary": "A general per-project land guard command"
    },
    {
      "label": "none",
      "summary": "No code; run contract bumps with --stop-after commit"
    }
  ],
  "recommendation": {
    "label": "probe",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "probe",
  "rationale": "Owner chose 'Contract probe command (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S353"
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
