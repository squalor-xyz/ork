---
{
  "id": 667,
  "title": "Which directory do the UI brief's render flag commands write to?",
  "status": "decided",
  "key": "ui.render-flag-dir",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S660 spec draft; supersedes adj 508 (tempdir).",
  "options": [
    {
      "label": "renders-dir-both",
      "summary": "the CLI and TUI -renders commands write straight into the renders directory; no copy step"
    },
    {
      "label": "renders-dir-tui",
      "summary": "TUI only"
    },
    {
      "label": "keep-tempdir",
      "summary": "keep adj 508"
    }
  ],
  "recommendation": {
    "label": "renders-dir-both",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "renders-dir-both",
  "rationale": "Owner chose 'Renders dir, both' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "supersedes": [
    508
  ],
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
    },
    {
      "date": "2026-11-27",
      "event": "supersede",
      "note": "supersedes 508"
    }
  ]
}
---
