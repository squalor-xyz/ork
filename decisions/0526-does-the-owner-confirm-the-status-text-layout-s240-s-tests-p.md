---
{
  "id": 526,
  "title": "Does the owner confirm the status-text layout S240's tests pin (Q-S1 to Q-S4)?",
  "status": "decided",
  "key": "ui.status-text-layout",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S240 spec check 2026-11-20: header 'needs you N, in progress M, active relays K'; 'waiting since HH:MM (AGE)' / 'updated HH:MM (AGE ago)'; one line per question line then next:; relay label; 'finding RUN N later|owner: TITLE (...)'; an owner command given a run- id gets an error hint.",
  "options": [
    {
      "label": "confirm",
      "summary": "The pinned layout stands"
    },
    {
      "label": "change",
      "summary": "The owner names changes first"
    }
  ],
  "recommendation": {
    "label": "confirm",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "confirm",
  "rationale": "Owner chose 'Confirm as pinned (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "narrowed_by": [
    {
      "id": 530,
      "scope": "a later finding's file command goes on its own indented line, not inline"
    }
  ],
  "refs": [
    "S240"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    },
    {
      "date": "2026-11-21",
      "event": "narrow",
      "note": "by 530: a later finding's file command goes on its own indented line, not inline"
    }
  ]
}
---
