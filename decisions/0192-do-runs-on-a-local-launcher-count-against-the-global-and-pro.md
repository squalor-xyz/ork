---
{
  "id": 192,
  "title": "Do runs on a local launcher count against the global and project caps?",
  "status": "decided",
  "key": "roles.local-model.cap-exempt",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-07",
  "created": "2026-11-07",
  "decided_at": "2026-11-07",
  "context": "Adj 187 sets one shared local pool of 4 but says nothing about the global cap (default 2) or a project cap. The owner wants frontier and local work to run side by side: with a project cap of 2 and a local pool of 4, two frontier slices and two local slices together.",
  "options": [
    {
      "label": "implementer-local",
      "summary": "A run whose implementer is a -local row takes a local-pool slot for the whole run (its frontier review and gate rounds included) and no global or project slot; other runs use the global and project caps as today"
    },
    {
      "label": "role-level",
      "summary": "No run slot while all active roles are local; a frontier role takes a global or project slot for its round"
    },
    {
      "label": "per-role-adds",
      "summary": "The run holds a global or project slot; local roles add a local-pool slot (no extra concurrency)"
    }
  ],
  "recommendation": {
    "label": "implementer-local",
    "reasoning": "Gives the side-by-side concurrency the owner asked for with the simplest rule."
  },
  "decision": "implementer-local",
  "rationale": "Owner picked this option in an interactive question on 2026-11-07 ('Run is local if its implementer is local'), after saying local agents must not count on the cap so 2 frontier and 2 local slices can run together.",
  "refs": [
    "S301",
    "187"
  ],
  "history": [
    {
      "date": "2026-11-07",
      "event": "ask"
    },
    {
      "date": "2026-11-07",
      "event": "decide"
    }
  ]
}
---
