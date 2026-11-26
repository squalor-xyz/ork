---
{
  "id": 599,
  "title": "What is a command name in a role prompt?",
  "status": "decided",
  "key": "review.stale-doc-command-name",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S569 spec draft.",
  "options": [
    {
      "label": "tool-subcommand",
      "summary": "first two lowercase words of a backticked span; a flag or uppercase second word gives no name"
    },
    {
      "label": "first-word",
      "summary": "first word only"
    },
    {
      "label": "whole-span",
      "summary": "whole backticked span"
    }
  ],
  "recommendation": {
    "label": "tool-subcommand",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "tool-subcommand",
  "rationale": "Owner chose 'Tool + subcommand' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S569"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
