---
{
  "id": 132,
  "title": "What may the owner change in a slice's lineup before launch?",
  "status": "decided",
  "key": "lineup.launch-edit",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Owner wants to set each slice's roles to another vendor, model or effort than the role matrix gives, for one launch. Record 24 says the chain is editable at launch; design.md:147-150 says the owner can edit both. Today the plan keeps only the first Claude row per role (internal/daemon/runs.go:225-253). The gate follows the reviewer (design.md:580, engine/states.go:248).",
  "options": [
    {
      "label": "free",
      "summary": "Set vendor, model and effort per role freely, seeded from the matrix row; the daemon refuses a vendor it has no adapter for (claude only until S47)"
    },
    {
      "label": "rows",
      "summary": "Pick each role's primary only from the rows relay-roles recommend returned (primary and alternatives); the plan must then also return the alternatives"
    },
    {
      "label": "rows-plus-free",
      "summary": "Pick from the matrix rows, with a free entry as an escape"
    }
  ],
  "recommendation": {
    "label": "free",
    "reasoning": "The owner asked for vendor, model and effort per role; the matrix stays the default and the run record shows what ran."
  },
  "decision": "free",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S80",
    "S83"
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
