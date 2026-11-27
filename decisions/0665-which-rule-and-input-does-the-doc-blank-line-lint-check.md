---
{
  "id": 665,
  "title": "Which rule and input does the doc blank-line lint check?",
  "status": "decided",
  "key": "review.doc-paragraph-blank-line-lint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S647 spec check.",
  "options": [
    {
      "label": "diff-rule",
      "summary": "a docscheck test diffs against main and flags an inserted doc line that touches an unchanged non-blank line; skips without git or main"
    },
    {
      "label": "marker-rule",
      "summary": "marker rule"
    },
    {
      "label": "other",
      "summary": "other"
    }
  ],
  "recommendation": {
    "label": "diff-rule",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "diff-rule",
  "rationale": "Owner chose 'Diff rule' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S647"
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
