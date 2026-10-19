---
{
  "id": 51,
  "title": "How does ork find projects before ork projects add exists (S03)?",
  "status": "decided",
  "key": "registry.seed",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "The registry is projects.json (docs/state.md); the CLI to edit it is in S03.",
  "options": [
    {
      "label": "seed",
      "summary": "seed the four decided projects on first run"
    },
    {
      "label": "manual",
      "summary": "refuse to start without projects.json and print an example"
    },
    {
      "label": "cli-in-s02",
      "summary": "move ork projects add into S02"
    }
  ],
  "decision": "seed",
  "rationale": "Owner 2026-10-19: If projects.json is missing, write it with the four decided projects whose paths exist, and say so; hand-edit after that.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
