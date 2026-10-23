---
{
  "id": 157,
  "title": "Does ork still seed four projects on first run?",
  "status": "open",
  "key": "registry.no-seed",
  "created": "2026-10-23",
  "context": "Owner 2026-10-23: a second PC with a different directory layout got an empty seeded registry (none of slicer, adjudicate, DataBall, agents under ~/code/squalor-xyz), written to projects.json and blocking re-seeding. Record 55 (registry.seed) seeds four decided projects. Owner: do not seed; ork opens blank and guides registering projects (TUI welcome with an add prompt); the registry file is written on the first add.",
  "options": [
    {
      "label": "no-seed",
      "summary": "Never seed; open blank, the TUI welcome adds projects, write projects.json on the first add",
      "tradeoffs": "Fresh machines need one add per project; no machine-specific defaults; supersedes record 55."
    },
    {
      "label": "keep-seed",
      "summary": "Keep seeding the four projects when they exist",
      "tradeoffs": "Zero setup on the owner's own layout; wrong or empty elsewhere."
    }
  ],
  "recommendation": {
    "label": "no-seed",
    "reasoning": "Owner's decision 2026-10-23; a registry should hold only what the machine has."
  },
  "refs": [
    "registry.seed"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    }
  ]
}
---
