---
{
  "id": 457,
  "title": "Do the implementer presets allow rg, given rg --pre runs a program?",
  "status": "decided",
  "key": "launch.allowlist-rg",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S409 review found rg --pre/--pre-glob (rg --help). adj 455 assumed rg is read-only.",
  "options": [
    {
      "label": "narrow",
      "summary": "Allow rg; --pre and --pre-glob lines are orchestrator-only and denied"
    },
    {
      "label": "keep",
      "summary": "Keep Bash(rg *) as decided in adj 455"
    },
    {
      "label": "drop",
      "summary": "Do not allow rg; rg Check lines stay orchestrator-only"
    }
  ],
  "recommendation": {
    "label": "narrow",
    "reasoning": "rg --pre=COMMAND runs a program on each file, so rg is not strictly read-only"
  },
  "decision": "drop",
  "rationale": "Owner chose 'Drop rg' in an interactive question after S409's review found rg --pre runs a program, relay session 8d711f67, 2026-11-17.",
  "supersedes": [
    455
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    },
    {
      "date": "2026-11-17",
      "event": "supersede",
      "note": "supersedes 455"
    }
  ]
}
---
