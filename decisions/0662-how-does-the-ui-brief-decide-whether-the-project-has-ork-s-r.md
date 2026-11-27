---
{
  "id": 662,
  "title": "How does the UI brief decide whether the project has ork's render commands, and what do other projects get?",
  "status": "decided",
  "key": "ui.brief-render-sections-gate",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S660 spec check.",
  "options": [
    {
      "label": "detect-plus-fallback",
      "summary": "write the CLI and TUI render sections only when the worktree has the render tests; otherwise a short generic Renders section pointing at the project's AGENTS.md and docs"
    },
    {
      "label": "detect-uimd",
      "summary": "generic wording in ui.md"
    },
    {
      "label": "registry-field",
      "summary": "a registry field"
    }
  ],
  "recommendation": {
    "label": "detect-plus-fallback",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "detect-plus-fallback",
  "rationale": "Owner chose 'Detect in worktree + brief fallback' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S660"
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
