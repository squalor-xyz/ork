---
{
  "id": 276,
  "title": "S327: how should the CHANGELOG resolver handle the mixed separator layout CHANGELOG.md already has?",
  "status": "decided",
  "key": "changelog.separator-style",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S327 implementer round 1 (run ork-20261111-031408-998a): the Unreleased body of CHANGELOG.md mixes entries joined with no blank line (26) and entries separated by a blank line (41), so the slice's refuse-mixed rule would still pause every landing.",
  "options": [
    {
      "label": "accept-mixed",
      "summary": "parse entries whatever the separators; old entries and separators stay byte for byte; new entries are joined with the base's separator at the top of Unreleased"
    },
    {
      "label": "normalise",
      "summary": "make the Unreleased body one style in this slice and keep the strict rule"
    },
    {
      "label": "as-written",
      "summary": "pure compact bodies only; the real file keeps pausing"
    }
  ],
  "recommendation": {
    "label": "accept-mixed",
    "reasoning": "Only option that stops landings pausing on the file as it is, with no large rewrite of CHANGELOG.md."
  },
  "decision": "accept-mixed",
  "rationale": "Owner picked Accept mixed (recommended) in an interactive question on 2026-11-11.",
  "refs": [
    "S327",
    "S180"
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
