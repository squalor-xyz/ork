---
{
  "id": 644,
  "title": "How does the UI pass render TUI views: overlay route or a test flag?",
  "status": "decided",
  "key": "roles.ui-pass-tui-render-flag",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S538 spec check; supersedes adj 507 (overlay).",
  "options": [
    {
      "label": "flag-mirror-cli-print",
      "summary": "internal/tui gets -renders DIR and -render-fixture NAME flags like internal/cli (80x24, 120x40, ascii), plus a mode that prints fixture text; the overlay recipe in AGENTS.md and ui.md is replaced"
    },
    {
      "label": "flag-only",
      "summary": "flags without the print mode"
    },
    {
      "label": "overlay",
      "summary": "keep the overlay route"
    }
  ],
  "recommendation": {
    "label": "flag-mirror-cli-print",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "flag-mirror-cli-print",
  "rationale": "Owner chose 'Flag, mirror CLI + print' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "supersedes": [
    507
  ],
  "refs": [
    "S538"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    },
    {
      "date": "2026-11-27",
      "event": "supersede",
      "note": "supersedes 507"
    }
  ]
}
---
