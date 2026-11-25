---
{
  "id": 546,
  "title": "How does the confirmed write get bound to the previewed result?",
  "status": "decided",
  "key": "matrix.copy-bind-preview",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: How does the confirmed write get bound to the previewed result?\nOptions:\n1. expect-steps: add the per-project step list from the preview to `MatrixCopyParams`. The daemon re-runs its dry run and refuses that project with a stale-preview error if the steps differ. It writes nothing for a refused project and goes on to the others, as `matrixCopy` does now.\n2. expect-revision: send only each project's `RevisionBefore` from the preview as `Expect`. This detects a changed target but not a changed plan, such as new source rows or different add ids.\n3. one-call: the daemon holds the previewed jobs under a token and the CLI confirms by token. This removes the second dry run but adds daemon state and a new method.\nRecommend: 1 - it binds exactly what the owner saw, needs no daemon state, and reuses the refuse-and-go-on handling already in `matrixCopy`.\nDecision key: matrix.copy-bind-preview",
  "decision": "expect-steps: add the per-project step list from the preview to `MatrixCopyParams`. The daemon re-runs its dry run and refuses that project with a stale-preview error if the steps differ. It writes nothing for a refused project and goes on to the others, as `matrixCopy` does now.",
  "rationale": "Answered in ork on 2026-11-25: How does the confirmed write get bound to the previewed result? (S548)",
  "refs": [
    "S548"
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
