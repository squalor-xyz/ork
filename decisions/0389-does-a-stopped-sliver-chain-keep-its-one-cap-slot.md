---
{
  "id": 389,
  "title": "Does a stopped sliver chain keep its one cap slot?",
  "status": "decided",
  "key": "slice.sliver-chain-stop-slot",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S62 implementer question (run ork-20261116-182129-0f4e). Narrows adj 387: the chain holds one slot while it runs.",
  "options": [
    {
      "label": "hold",
      "summary": "keep the slot until the last sliver ends or the owner dismisses the chain"
    },
    {
      "label": "release",
      "summary": "free the slot when a sliver stops, and take a slot again on retry"
    }
  ],
  "recommendation": {
    "label": "release",
    "reasoning": "a stopped ordinary run holds no slot and ork retry gets past the caps (docs/design.md:1119-1121); a stopped chain should not block other work"
  },
  "decision": "release",
  "rationale": "Owner chose 'Release (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16; narrows adj 387.",
  "refs": [
    "S62"
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
