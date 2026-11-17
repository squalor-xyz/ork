---
{
  "id": 403,
  "title": "What happens to ork's CHANGELOG merge resolver once ork uses landed-changed/?",
  "status": "decided",
  "key": "changelog.resolver-fate",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S332 spec question, 2026-11-17.",
  "options": [
    {
      "label": "keep-fallback",
      "summary": "keep it for projects without landed-changed/"
    },
    {
      "label": "remove",
      "summary": "remove it"
    }
  ],
  "recommendation": {
    "label": "keep-fallback",
    "reasoning": "ork runs other projects that have not adopted landed-changed/"
  },
  "decision": "keep-fallback",
  "rationale": "Owner chose 'Keep as fallback (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S332"
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
