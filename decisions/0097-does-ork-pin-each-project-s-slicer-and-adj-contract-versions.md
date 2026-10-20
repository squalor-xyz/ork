---
{
  "id": 97,
  "title": "Does ork pin each project's slicer and adj contract versions and check every pin?",
  "status": "decided",
  "key": "boundary.contract-pin",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "slicer s387 adds a relay_contract pin; adjudicate S44 adds an adj-json contract version and pin; ork S24 sets and checks both.",
  "options": [
    {
      "label": "ork-sets-checks",
      "summary": "Each tool keeps its pin in its own committed config (slicer relay_contract, adj contract); ork projects pin sets them through the tools; doctor and engine start check pin in accepted set and equal to what each tool prints",
      "tradeoffs": "One pin per tool per project, travels with the repo; ork runs other tools' write commands"
    },
    {
      "label": "ork-reads-only",
      "summary": "Each tool keeps its pin; a person sets them; ork only checks",
      "tradeoffs": "No writes from ork; pinning is manual across tools"
    },
    {
      "label": "ork-own-pin",
      "summary": "ork also stores a per-project pin in projects.json and doctor reports disagreement",
      "tradeoffs": "Three places that can drift"
    }
  ],
  "recommendation": {
    "label": "ork-sets-checks",
    "reasoning": "Owner choice in the slicer session 2026-10-20: per-contract pins, each tool keeps its own pin, ork sets and checks them. Follows record 67 (accepted-set); does not supersede it."
  },
  "decision": "ork-sets-checks",
  "rationale": "Owner 2026-10-20: agrees with the recommendation. The ref to adjudicate S44 should read S45 (the item that adds the pin); that edit is the owner's.",
  "refs": [
    "S24",
    "../slicer:s387",
    "../adjudicate:S44"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
