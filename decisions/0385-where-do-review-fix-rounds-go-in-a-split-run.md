---
{
  "id": 385,
  "title": "Where do review fix rounds go in a split run?",
  "status": "decided",
  "key": "split.fix-round-routing",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S61 implementer question (run ork-20261116-164252-2a48). slicer's relay contract says accepted findings go back to the part that owns the file.",
  "options": [
    {
      "label": "lead",
      "summary": "the lead gets every fix round"
    },
    {
      "label": "owning-part",
      "summary": "each fix goes to the part whose file it names; the lead gets the rest (slicer contract, ai.py:920)"
    },
    {
      "label": "lead-decides",
      "summary": "lead by default; the lead may hand a file back to its part per fix round"
    }
  ],
  "recommendation": {
    "label": "lead",
    "reasoning": "one fix path; the lead owns tracking and the combined diff"
  },
  "decision": "lead-decides",
  "rationale": "Owner chose 'Lead decides' (not the recommendation) in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S61"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
