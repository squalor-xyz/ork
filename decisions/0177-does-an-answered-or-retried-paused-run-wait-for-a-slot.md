---
{
  "id": 177,
  "title": "Does an answered or retried paused run wait for a slot?",
  "status": "decided",
  "key": "wave.resume-slot",
  "asked_by": "Claude",
  "decided_by": "Jon",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "S274. A paused run holds no slot (adj 122), so other runs start while it waits; answer and retry then bypass the caps and can run over a cap (slicer s443, 2026-11-03).",
  "options": [
    {
      "label": "wait-first",
      "summary": "A paused run holds no slot; answer and retry of a paused run wait for room under the project and global caps and start ahead of queued runs; approve still bypasses"
    },
    {
      "label": "bypass",
      "summary": "As adj 122: answer and retry bypass the caps"
    }
  ],
  "recommendation": {
    "label": "wait-first",
    "reasoning": "Keeps the caps true without starving other projects; resumed work goes first."
  },
  "decision": "wait-first",
  "rationale": "Owner 2026-11-05/06, interactive questions in the ork session: paused run does not count for its own project cap; resumed first; retry/answer under both caps, approve bypasses. Narrows adj 122 for answer and retry.",
  "refs": [
    "S274",
    "adj:122"
  ],
  "history": [
    {
      "date": "2026-11-06",
      "event": "ask"
    },
    {
      "date": "2026-11-06",
      "event": "decide"
    }
  ]
}
---
