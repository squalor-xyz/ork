---
{
  "id": 321,
  "title": "S347: what does ork's launch tracking commit do when main's index lists other items whose slice files are untracked?",
  "status": "open",
  "key": "landing.launch-index-orphans",
  "asked_by": "claude",
  "created": "2026-11-11",
  "context": "2026-11-11: S312, S315 and S123 landings went red after launch commits a56c3f8 and d5880ab committed an index listing S345/S346 without their slice files.",
  "options": [
    {
      "label": "commit-listed",
      "summary": "also commit each listed item's untracked slice JSON and render, so main stays consistent"
    },
    {
      "label": "skip-index",
      "summary": "leave the index out of the launch commit; pause the run with a reason when the item needs it"
    },
    {
      "label": "refuse",
      "summary": "refuse the launch with a reason naming the items"
    }
  ],
  "recommendation": {
    "label": "commit-listed",
    "reasoning": "The files are slicer's own tracking files for items the committed index already lists; committing them keeps main checkable and needs no owner step."
  },
  "refs": [
    "S347"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    }
  ]
}
---
