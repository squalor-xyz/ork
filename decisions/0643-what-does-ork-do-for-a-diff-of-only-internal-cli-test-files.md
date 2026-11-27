---
{
  "id": 643,
  "title": "What does ork do for a diff of only internal/cli test files and docs, and for CLI-only output changes?",
  "status": "decided",
  "key": "ui.pass-cli-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S450 spec check (S540 merged).",
  "options": [
    {
      "label": "note-plus-one-file",
      "summary": "still run the UI pass with a No renders expected section for internal/cli test and docs-only diffs (tracking paths skipped); for CLI-only output changes say the 80x24, 120x40 and ascii renders are identical and write one file per view"
    },
    {
      "label": "note-only",
      "summary": "no-renders note only"
    },
    {
      "label": "skip",
      "summary": "skip the UI pass"
    }
  ],
  "recommendation": {
    "label": "note-plus-one-file",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "note-plus-one-file",
  "rationale": "Owner chose 'Note + S540 rule' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S450"
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
