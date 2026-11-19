---
{
  "id": 499,
  "title": "Does the read-only allowlist include adj lint?",
  "status": "decided",
  "key": "roles.read-only-adj-lint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "Since S418 cloud Claude read-only roles use LocalReadOnlyAllowlist (internal/engine/perm.go:23-33, adj 307), which has adj show and adj check but not adj lint; reviewers were denied adj lint --root (run ork-20261118-225932-056b).",
  "options": [
    {
      "label": "add",
      "summary": "Add Bash(adj lint*) to the read-only allowlist for local and cloud read-only roles"
    },
    {
      "label": "leave",
      "summary": "Leave it out"
    }
  ],
  "recommendation": {
    "label": "add",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "add",
  "rationale": "Owner chose 'Add adj lint (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S458"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
