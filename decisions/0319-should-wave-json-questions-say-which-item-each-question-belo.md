---
{
  "id": 319,
  "title": "Should wave --json questions say which item each question belongs to?",
  "status": "decided",
  "key": "specs.questions-json-shape",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S312 UI pass, run ork-20261111-215044-9784, 2026-11-11: with two or more wave items a script cannot tell which item a question belongs to or its #N address (wave.go:506-511). answered entries stay {item, key, answer} as adj 310 decided.",
  "options": [
    {
      "label": "keep",
      "summary": "flat engine.SpecQuestion list as adj 310 decided; scripts use text output for waves"
    },
    {
      "label": "item-field",
      "summary": "each questions entry is {item, address, ask, key, text}"
    },
    {
      "label": "grouped",
      "summary": "questions becomes a list of {item, questions:[...]}"
    }
  ],
  "recommendation": {
    "label": "item-field",
    "reasoning": "One flat list that matches answered's {item, key} and gives the exact address to pass to --answer."
  },
  "decision": "item-field",
  "rationale": "Owner picked item-field (Recommended) for S312's wave --json questions; recorded by the agent as the owner's delegate (adj 316).",
  "supersedes": [
    310
  ],
  "refs": [
    "S312"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    },
    {
      "date": "2026-11-11",
      "event": "supersede",
      "note": "supersedes 310"
    }
  ]
}
---
