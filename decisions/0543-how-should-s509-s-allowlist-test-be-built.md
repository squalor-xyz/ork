---
{
  "id": 543,
  "title": "How should S509's allowlist test be built?",
  "status": "decided",
  "key": "tests.allowlist-write-commands",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "S509 spec check: Files, Failing tests, Implement and Check were empty.",
  "options": [
    {
      "label": "extend",
      "summary": "extend TestReadOnlyAllowlistMatchesNoTrackingWrite in internal/engine/perm_test.go with slicer note, done, set, ai relay --init-roles, changelog add over LocalReadOnlyAllowlist and CloudUIAllowlist"
    },
    {
      "label": "real-matcher",
      "summary": "use Claude Code's real permission matcher"
    },
    {
      "label": "retire",
      "summary": "covered by S505's round-2 test"
    }
  ],
  "recommendation": {
    "label": "extend",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "extend",
  "rationale": "Owner chose 'Extend existing test' in an interactive question, relay session 8d711f67, 2026-11-24.",
  "refs": [
    "S509"
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
