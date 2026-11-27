---
{
  "id": 632,
  "title": "Tool feedback (feature, ork): in Base overlays, include a short sample of real go vet type-error output for one changed ",
  "status": "open",
  "key": "tool-feedback.ork-3c430485",
  "created": "2026-11-27",
  "context": "Proposal: in Base overlays, include a short sample of real go vet type-error output for one changed file so reviewers can confirm vet path forms without writing a probe overlay\nRole: reviewer\nRun: ork-20261127-163048-8b5b\nProject: ork\nItem: S634\nEvidence: I needed a hand-built empty-file overlay to see that real vet prints \"vet: internal/engine/round.go:628:19: ...\"",
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
    "S634"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    }
  ]
}
---
