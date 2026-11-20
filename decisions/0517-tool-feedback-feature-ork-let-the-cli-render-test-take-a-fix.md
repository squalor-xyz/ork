---
{
  "id": 517,
  "title": "Tool feedback (feature, ork): let the CLI render test take a fixture with real-length ids, titles and paths so width pro",
  "status": "decided",
  "key": "tool-feedback.ork-84b85e8a",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "Proposal: let the CLI render test take a fixture with real-length ids, titles and paths so width problems show in the default render\nRole: ui\nRun: ork-20261120-123026-80f4\nProject: ork\nItem: S318\nEvidence: the default summary fixture uses ids like r1 and S7, so every row fits; real-sized data in my overlay gave rows of about 98, 112 and 167 columns",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "decision": "file",
  "rationale": "Owner chose 'Fold both into S499 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20: part of S499, not its own slice.",
  "refs": [
    "S318"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
