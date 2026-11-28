---
{
  "id": 669,
  "title": "Which per-test run pattern and cap does the Base overlays section use, and where does the vet sample come from?",
  "status": "decided",
  "key": "base-overlays.per-test-run-form",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S643 spec draft: the anchored form of adj 663 is denied for read-only roles (S610).",
  "options": [
    {
      "label": "unanchored-cap20-live",
      "summary": "one go test -overlay -count=1 -run TestName line per test in the changed test files, capped at 20 with a left-out count; the live vet sample of adj 663 stays"
    },
    {
      "label": "anchored-permit",
      "summary": "anchored and permit it"
    },
    {
      "label": "unanchored-nocap",
      "summary": "no cap"
    }
  ],
  "recommendation": {
    "label": "unanchored-cap20-live",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "unanchored-cap20-live",
  "rationale": "Owner chose 'Unanchored, cap 20' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "supersedes": [
    663
  ],
  "refs": [
    "S643"
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
      "note": "supersedes 663"
    }
  ]
}
---
