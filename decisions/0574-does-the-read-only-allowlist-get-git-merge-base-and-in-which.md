---
{
  "id": 574,
  "title": "Does the read-only allowlist get git merge-base, and in which forms?",
  "status": "decided",
  "key": "launch.read-only-merge-base",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S575 tool feedback: a reviewer was denied git -C ... merge-base.",
  "options": [
    {
      "label": "both",
      "summary": "add Bash(git merge-base*) and Bash(git -C AGENTS.md CHANGELOG.md CLAUDE.md LICENSE NOTICE PROJECT.md README.md assets build-in-home-dir.sh build-only-in-home-dir.sh cmd decisions docs go.mod go.sum human-checks-todo.md internal landed-changed run-in-home-dir.sh spike testdata merge-base*) to LocalReadOnlyAllowlist"
    },
    {
      "label": "c-only",
      "summary": "only the -C form"
    },
    {
      "label": "leave-out",
      "summary": "keep the list as is"
    }
  ],
  "recommendation": {
    "label": "both",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "both",
  "rationale": "Owner chose 'Add both forms' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S575"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
