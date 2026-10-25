---
{
  "id": 164,
  "title": "Should ork check slice specs in a batch before a wave?",
  "status": "decided",
  "key": "roles.spec-check",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Retrospective R2 (docs/review-2026-10-25-relay-retrospective.md). Orchestrator preflight caught real gaps (S32, S97, S07, S33, S35, S45) but asked design questions one slice at a time; runs waiting on owner answers took 18-19 h against a 20 min median.",
  "options": [
    {
      "label": "batch-spec-check",
      "summary": "a read-only spec-check role reads the next slices and the code and returns per slice ready or structured questions; the owner answers them in one sitting in ork; only ready slices enter a wave; line anchors are refreshed by the implementer at the start of its round"
    },
    {
      "label": "none",
      "summary": "keep spec checks outside ork"
    }
  ],
  "recommendation": {
    "label": "batch-spec-check",
    "reasoning": "Moves owner waits out of running relays and keeps the gap-finding that paid off. Model proposed: sonnet medium, measured on the first waves."
  },
  "decision": "batch-spec-check",
  "rationale": "Owner approved the recommendation in the agent session on 2026-10-25 (AskUserQuestion: yes, decide all 162-170); recorded by the agent at the owner's direction. ",
  "refs": [
    "S111"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    },
    {
      "date": "2026-10-25",
      "event": "decide"
    }
  ]
}
---
