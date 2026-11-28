---
{
  "id": 681,
  "title": "Which fix for tests that need a temp dir outside any git repo?",
  "status": "decided",
  "key": "tests.non-repo-temp-dir",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S664 spec check.",
  "options": [
    {
      "label": "git-ceiling",
      "summary": "tests that need a non-repo temp dir set GIT_CEILING_DIRECTORIES, as internal/docscheck/blankline_test.go:219 does"
    },
    {
      "label": "document",
      "summary": "document TMPDIR placement"
    },
    {
      "label": "both",
      "summary": "both"
    }
  ],
  "recommendation": {
    "label": "git-ceiling",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "git-ceiling",
  "rationale": "Owner chose 'Set GIT_CEILING_DIRECTORIES' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S664"
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
