---
{
  "id": 630,
  "title": "How does a changed Go file whose own revert does not build get a base overlay that builds?",
  "status": "decided",
  "key": "base-overlays.dependent-revert",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S634 spec check (adj 620).",
  "options": [
    {
      "label": "named-file-closure",
      "summary": "add the base copies of changed Go files the vet output names, re-vet and repeat (capped); entry reads file (with other.go reverted too)"
    },
    {
      "label": "all-together",
      "summary": "all-files overlay on any failure"
    },
    {
      "label": "same-package",
      "summary": "revert the same package's changed files"
    },
    {
      "label": "retire",
      "summary": "leave it"
    }
  ],
  "recommendation": {
    "label": "named-file-closure",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "named-file-closure",
  "rationale": "Owner chose 'Named-file closure' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S634"
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
