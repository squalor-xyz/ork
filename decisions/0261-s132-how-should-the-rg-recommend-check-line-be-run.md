---
{
  "id": 261,
  "title": "S132: how should the rg Recommend Check line be run?",
  "status": "decided",
  "key": "process.s132-check-line",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-10",
  "created": "2026-11-10",
  "decided_at": "2026-11-10",
  "context": "Raised by the S132 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "replace with git diff -U0 -- docs/design.md, done when the diff adds the dash note at the two Recommend: passages"
    },
    {
      "label": "2",
      "summary": "orchestrator runs the rg line"
    },
    {
      "label": "3",
      "summary": "as written"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-10.",
  "refs": [
    "S132"
  ],
  "history": [
    {
      "date": "2026-11-10",
      "event": "ask"
    },
    {
      "date": "2026-11-10",
      "event": "decide"
    }
  ]
}
---
