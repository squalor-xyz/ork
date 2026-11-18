---
{
  "id": 462,
  "title": "How should ork tracking commit keep a long commit subject short?",
  "status": "decided",
  "key": "cli.tracking-commit-subject",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S405 implementer (2026-11-18): the commit subject and result line list every slice ID and adj number; a spec pass with about 40 adj records gives 250+ characters.",
  "options": [
    {
      "label": "full",
      "summary": "keep every ID and number"
    },
    {
      "label": "ranges",
      "summary": "fold consecutive runs into ranges, such as S387-S389; adj 402-405, 410-446"
    },
    {
      "label": "cap",
      "summary": "name the first N, then +M more, like nameList"
    }
  ],
  "recommendation": {
    "label": "ranges",
    "reasoning": "Names every item, as adj 453 asks, and keeps the usual commit near one line."
  },
  "decision": "ranges",
  "rationale": "Owner chose 'Fold into ranges (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S405"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
