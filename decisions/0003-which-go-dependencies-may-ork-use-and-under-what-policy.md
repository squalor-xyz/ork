---
{
  "id": 3,
  "title": "Which Go dependencies may ork use, and under what policy?",
  "status": "decided",
  "key": "stack.go-deps",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Owner answer 2026-10-18: Go, with some few chosen select dependencies. adjudicate is stdlib only (C24); global rules require approval per dependency.",
  "options": [
    {
      "label": "per-dep-approval",
      "summary": "Stdlib by default; each dependency approved one at a time with why-not-stdlib, license and carrying cost"
    },
    {
      "label": "stdlib-only",
      "summary": "Go standard library only, like adjudicate"
    }
  ],
  "recommendation": {
    "label": "per-dep-approval",
    "reasoning": "Matches the owner's answer and the global dependency rules."
  },
  "decision": "per-dep-approval",
  "rationale": "Owner interview 2026-10-18: approved Bubble Tea, Lip Gloss and Bubbles for the TUI, void if a license check fails. Anything else is asked separately.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
