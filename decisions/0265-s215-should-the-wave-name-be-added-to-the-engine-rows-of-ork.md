---
{
  "id": 265,
  "title": "S215: should the wave name be added to the engine rows of ork status --json?",
  "status": "decided",
  "key": "wave.name-in-status",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-10",
  "created": "2026-11-10",
  "decided_at": "2026-11-10",
  "context": "Raised by the S215 implementer; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "leave it: the name stays in run.json only"
    },
    {
      "label": "2",
      "summary": "follow-up slice adds wave and wave_name to source.EngineRun and engineRuns"
    },
    {
      "label": "3",
      "summary": "add them with the TUI slice (S216)"
    }
  ],
  "recommendation": {
    "label": "2",
    "reasoning": "Implementer recommendation."
  },
  "decision": "2",
  "rationale": "Owner picked option 2 on 2026-11-10.",
  "refs": [
    "S215"
  ],
  "history": [
    {
      "date": "2026-11-10",
      "event": "ask"
    },
    {
      "date": "2026-11-10",
      "event": "decide"
    }
  ]
}
---
