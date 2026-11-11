---
{
  "id": 293,
  "title": "S328: how does ork doctor --fix get the merge-driver git config lines?",
  "status": "decided",
  "key": "doctor.merge-driver-fix-source",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "via-slicer",
      "summary": "run slicer setup-git --root P, keep only git config merge.NAME.(name|driver) VALUE lines, run each as git -C P config after one [y/N] per project"
    },
    {
      "label": "hardcoded",
      "summary": "ork carries the six lines"
    },
    {
      "label": "gitattributes",
      "summary": "parse the comments in .slicer/.gitattributes"
    }
  ],
  "recommendation": {
    "label": "via-slicer",
    "reasoning": "slicer owns the driver names; nothing drifts."
  },
  "decision": "via-slicer",
  "rationale": "Owner picked Ask slicer (recommended).",
  "refs": [
    "S328"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
