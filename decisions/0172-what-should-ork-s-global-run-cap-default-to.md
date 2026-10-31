---
{
  "id": 172,
  "title": "What should ork's global run cap default to?",
  "status": "decided",
  "key": "engine.default-cap",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-26",
  "decided_at": "2026-10-31",
  "context": "docs/design.md:204 sets the global cap at 4. The cap decides how many runs hold a slot at once (adj 122).",
  "options": [
    {
      "label": "one",
      "summary": "Default 1; the owner raises it per wave or in config"
    },
    {
      "label": "two",
      "summary": "Default 2"
    },
    {
      "label": "four",
      "summary": "Keep the default at 4"
    }
  ],
  "recommendation": {
    "label": "one",
    "reasoning": "The 2026-10-26 orchestration trial ran five slices one at a time with no stalls and no merge conflicts, and the earlier parallel stretch had three landing conflicts (S121, S85, S119); the owner said there was too much going on in parallel. See docs/review-2026-10-26-orchestration-trial.md (O2)."
  },
  "decision": "two",
  "rationale": "Owner picked this option in an interactive question in the v1.0 release session on 2026-10-31 (AskUserQuestion), against the recommendation (one).",
  "history": [
    {
      "date": "2026-10-26",
      "event": "ask"
    },
    {
      "date": "2026-10-31",
      "event": "decide"
    }
  ]
}
---
