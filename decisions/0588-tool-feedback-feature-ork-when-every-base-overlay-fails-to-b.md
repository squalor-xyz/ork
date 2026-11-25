---
{
  "id": 588,
  "title": "Tool feedback (feature, ork): when every base overlay fails to build because of a shared signature change, offer one ove",
  "status": "decided",
  "key": "tool-feedback.ork-2c0eabd5",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: when every base overlay fails to build because of a shared signature change, offer one overlay that reverts all changed product files together\nRole: reviewer\nRun: ork-20261125-211628-0c81\nProject: ork\nItem: S587\nEvidence: all four Base overlays entries in the S587 brief say \"does not build\" because baseOverlaySection, untestedCallSitesSection and branchChangeResult change together (see S596)",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "decision": "drop",
  "rationale": "Folded into S596 (not a separate slice): Jon chose 'Two slices' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S587"
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
