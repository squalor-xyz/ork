---
{
  "id": 533,
  "title": "How should long help lines wrap? Word wrapping can leave a key letter at the end of one line with its label on the next: action-help-mono-true.txt shows `(Running); E` and then `  expand or collapse all`, and roadmap-help-80x24-mono.txt shows `n new` and then `  slice`.",
  "status": "decided",
  "key": "ui.help-wrap",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: How should long help lines wrap? Word wrapping can leave a key letter at the end of one line with its label on the next: action-help-mono-true.txt shows `(Running); E` and then `  expand or collapse all`, and roadmap-help-80x24-mono.txt shows `n new` and then `  slice`.\nOptions:\n1. words: keep the plain word wrap the spec asked for, as now\n2. hints: break at `; ` between hints first, and split words only when one hint is wider than the line\nRecommend: 2 - a key stays on the same line as its label, so the help reads by key, which is the point of the overlay\nDecision key: ui.help-wrap",
  "decision": "2 - break at '; ' between hints first, and split words only when one hint is wider than the line (owner, 2026-11-25)",
  "rationale": "Answered in ork on 2026-11-25: How should long help lines wrap? Word wrapping can leave a key letter at the end of one line with its label on the next: action-help-mono-true.txt shows `(Running); E` and then `  expand or collapse all`, and roadmap-help-80x24-mono.txt shows `n new` and then `  slice`.",
  "refs": [
    "S401"
  ],
  "history": [
    {
      "date": "2026-11-24",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
