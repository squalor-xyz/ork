---
{
  "id": 179,
  "title": "Which CLIs does a local-model row cover?",
  "status": "decided",
  "key": "roles.local-model.scope",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "S301. The launchers claude-local and codex-local exist, and Grok has the local model in its regular config.",
  "options": [
    {
      "label": "all-three",
      "summary": "claude, codex and grok"
    },
    {
      "label": "claude-only",
      "summary": "claude only; codex and grok in a later slice"
    }
  ],
  "recommendation": {
    "label": "claude-only",
    "reasoning": "Codex and Grok need config files outside the repo; the owner says the launchers are already set up, so all three is feasible."
  },
  "decision": "All three: claude, codex and grok. The launchers are already set up; Grok has the local model in its regular config.",
  "rationale": "Owner answer, interactive question, 2026-11-06.",
  "refs": [
    "S301"
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
