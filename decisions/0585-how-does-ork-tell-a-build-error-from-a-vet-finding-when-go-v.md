---
{
  "id": 585,
  "title": "How does ork tell a build error from a vet finding when go vet exits non-zero on a base overlay?",
  "status": "decided",
  "key": "review.base-overlay-build-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S593 spec draft.",
  "options": [
    {
      "label": "any-nonzero",
      "summary": "any non-zero go vet exit is does not build: ERROR with the first output lines"
    },
    {
      "label": "compile-only",
      "summary": "go test -c -vet=off -overlay"
    },
    {
      "label": "label-both",
      "summary": "label does not build or fails go vet"
    }
  ],
  "recommendation": {
    "label": "any-nonzero",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "any-nonzero",
  "rationale": "Owner chose 'Any non-zero exit' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S593"
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
