---
{
  "id": 634,
  "title": "Does S527 cover go doc, slicer relay-roles show and slicer writes in the temp dir?",
  "status": "decided",
  "key": "roles.read-only-allowlist-additions",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S527 spec check after the triage merge of S541 and S528.",
  "options": [
    {
      "label": "all-three",
      "summary": "go doc (and the pinned go -C form), slicer relay-roles show, and slicer init and writes with --root inside the role temp dir, with refusal tests"
    },
    {
      "label": "go-doc-only",
      "summary": "go doc only"
    }
  ],
  "recommendation": {
    "label": "all-three",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "all-three",
  "rationale": "Owner chose 'All three' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S527"
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
