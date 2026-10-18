---
{
  "id": 1,
  "title": "What does ork do first once implementation starts?",
  "status": "decided",
  "key": "scope.v1",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Owner chose docs/spec only on 2026-10-18, then set the main goal: a cross-project TUI dashboard that shows roadmaps, decisions and live agents, edits roadmaps and decisions, and launches and supervises relays (PROJECT.md Goals). Candidates are in docs/manual-steps.md and docs/dashboard.md. slicer s322-s329 already cover many outer-session steps.",
  "options": [
    {
      "label": "spec-only",
      "summary": "Stay docs and spec only until further notice (owner's 2026-10-18 answer)"
    },
    {
      "label": "outer-session-driver",
      "summary": "Sequence the outer session's deterministic steps (setup, worktree, relay-run begin/end, report checks, note filing, batched landing)"
    },
    {
      "label": "landing-only",
      "summary": "Start with batched landing and changelog conflict resolution only"
    }
  ],
  "recommendation": {
    "label": "spec-only",
    "reasoning": "Recorded from the owner's answer in the setup session; revisit when a first capability is wanted."
  },
  "decision": "v1 = monitoring dashboard across projects + launching and supervising slice waves. Roadmap and decision editing wait for a later version.",
  "rationale": "Owner interview 2026-10-18: v1 targets monitoring and launching; editing can wait.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "amend",
      "note": "context",
      "was": {
        "context": "Owner chose docs/spec only on 2026-10-18. Candidates are in docs/manual-steps.md. slicer s322-s329 already cover many outer-session steps."
      }
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
