---
{
  "id": 362,
  "title": "When a decided adj record's option still fits a keyed spec question, is the question hidden or shown with press-enter-to-accept?",
  "status": "decided",
  "key": "speccheck.decided-reuse-display",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S313 spec check question. Narrows adj 227's 'press enter to accept' wording to the mismatch case.",
  "options": [
    {
      "label": "hide-when-fits",
      "summary": "reuse silently when the decided option is one of the question's options; show 'decided as X on DATE' and ask again only when it no longer exists"
    },
    {
      "label": "always-show",
      "summary": "show every decided keyed question as 'decided as X on DATE, press enter to accept' (adj 227 as worded)"
    },
    {
      "label": "as-written",
      "summary": "keep the tests; no override for a decided record that still fits"
    }
  ],
  "recommendation": {
    "label": "hide-when-fits",
    "reasoning": "matches the failing tests; an option mismatch is the only staleness ork can detect"
  },
  "decision": "hide-when-fits",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-15 ('Hide when it fits (Recommended)'); narrows adj 227 to the mismatch case.",
  "refs": [
    "S313"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
