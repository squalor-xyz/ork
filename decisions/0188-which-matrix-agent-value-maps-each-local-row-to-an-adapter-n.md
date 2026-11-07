---
{
  "id": 188,
  "title": "Which matrix Agent value maps each local row to an adapter, now that all three have launchers?",
  "status": "decided",
  "key": "roles.local-model.agent-value-v2",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-07",
  "created": "2026-11-07",
  "decided_at": "2026-11-07",
  "context": "Replaces 184 and 185 (Grok via -m studio-local). The launchers are scripts the owner puts on PATH.",
  "options": [
    {
      "label": "launcher-names",
      "summary": "claude-local, codex-local and grok-local; each is the adapter binary and the lineup vendor; ork passes no -m studio-local"
    },
    {
      "label": "grok-via-model",
      "summary": "keep Agent Grok with -m studio-local"
    }
  ],
  "recommendation": {
    "label": "launcher-names",
    "reasoning": "Owner answer 2026-11-06: grok-local is a launcher like the other two."
  },
  "decision": "launcher-names",
  "rationale": "Owner picked Agent grok-local (2026-11-06) and asked on 2026-11-07 to supersede 184 and 185.",
  "supersedes": [
    184,
    185
  ],
  "refs": [
    "S301",
    "adj:155",
    "adj:184",
    "adj:185"
  ],
  "history": [
    {
      "date": "2026-11-07",
      "event": "ask"
    },
    {
      "date": "2026-11-07",
      "event": "decide"
    },
    {
      "date": "2026-11-07",
      "event": "supersede",
      "note": "supersedes 184"
    },
    {
      "date": "2026-11-07",
      "event": "supersede",
      "note": "supersedes 185"
    }
  ]
}
---
