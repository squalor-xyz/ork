---
{
  "id": 673,
  "title": "Tool feedback (feature, ork): when a slice gates one brief sentence on vendor, also list other sentences in the same blo",
  "status": "open",
  "key": "tool-feedback.ork-52222093",
  "created": "2026-11-27",
  "context": "Proposal: when a slice gates one brief sentence on vendor, also list other sentences in the same block that refer to the gated resource\nRole: reviewer\nRun: ork-20261128-002830-34c6cdfa\nProject: ork\nItem: S648\nEvidence: brief.go:237 and brief.go:240 still mention \"the renders directory\" after its definition at brief.go:233 was gated on claude",
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
  "refs": [
    "S648"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    }
  ]
}
---
