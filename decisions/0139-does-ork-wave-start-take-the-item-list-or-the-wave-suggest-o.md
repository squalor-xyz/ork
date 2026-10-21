---
{
  "id": 139,
  "title": "Does ork wave start take the item list or the wave suggest output?",
  "status": "open",
  "key": "wave.start-input",
  "asked_by": "claude",
  "created": "2026-10-20",
  "context": "S60 open (c). The Waves screen (S82) launches the ticked items through the same wave.start method, so the daemon method takes items either way.",
  "options": [
    {
      "label": "items",
      "summary": "ork wave start PROJECT ITEM... takes the item ids the owner chose; wave suggest output is read by the owner",
      "tradeoffs": "Stateless, matches the TUI ticking items, owner can edit the set"
    },
    {
      "label": "suggest-output",
      "summary": "ork wave start takes a suggested wave number from the last wave suggest",
      "tradeoffs": "Shorter to type, needs the suggestion kept between calls and can go stale"
    }
  ],
  "recommendation": {
    "label": "items",
    "reasoning": "Stateless and the same shape the Waves screen needs."
  },
  "refs": [
    "S60",
    "S82"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    }
  ]
}
---
