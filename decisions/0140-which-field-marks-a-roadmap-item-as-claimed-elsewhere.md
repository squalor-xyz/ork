---
{
  "id": 140,
  "title": "Which field marks a roadmap item as claimed elsewhere?",
  "status": "open",
  "key": "roadmap.readiness",
  "asked_by": "claude",
  "created": "2026-10-20",
  "context": "S12 open: slicer list --json --lean --all and the lean deps output have no claim or in_work_elsewhere field (checked when S12 was specced), so the Roadmap screen cannot show readiness from claims yet.",
  "options": [
    {
      "label": "no-claim",
      "summary": "Show readiness from status and dependencies only; leave claim out until slicer provides a field",
      "tradeoffs": "S12 can land now; a claimed item may show as ready"
    },
    {
      "label": "slicer-field",
      "summary": "Wait for slicer to add a claim field, then use it",
      "tradeoffs": "Exact, but blocks S12 and everything after it until slicer ships"
    }
  ],
  "recommendation": {
    "label": "no-claim",
    "reasoning": "Does not block the Waves chain; the claim display can be added later."
  },
  "refs": [
    "S12"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    }
  ]
}
---
