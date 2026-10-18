---
{
  "id": 9,
  "title": "What do ork's own state files hold?",
  "status": "decided",
  "key": "state.files",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Owner answer 2026-10-18. Details in docs/state.md.",
  "options": [
    {
      "label": "registry-runs",
      "summary": "Project registry, ork run records and UI preferences in XDG dirs; everything else read live"
    },
    {
      "label": "snapshot",
      "summary": "Also a collated snapshot of every project's roadmap and decisions"
    },
    {
      "label": "git-repo",
      "summary": "Registry and runs in a git-tracked state repo"
    }
  ],
  "recommendation": {
    "label": "registry-runs",
    "reasoning": "Owner's 2026-10-18 answer."
  },
  "decision": "registry-runs",
  "rationale": "Owner interview 2026-10-18: registry, run records and UI preferences in XDG dirs; everything else read live.",
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
