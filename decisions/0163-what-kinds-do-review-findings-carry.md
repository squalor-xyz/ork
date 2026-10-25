---
{
  "id": 163,
  "title": "What kinds do review findings carry?",
  "status": "decided",
  "key": "review.finding-kinds",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Retrospective R4 (docs/review-2026-10-25-relay-retrospective.md). Reviewer and UI pass findings mixed design calls into fix rounds (S65 spent grant) or left them in later notes only the outer session read (S84 UI suggestions).",
  "options": [
    {
      "label": "fix-owner-later",
      "summary": "each finding is fix (goes to the fix round), owner (a structured owner question; blocks the gate only when marked a blocker) or later (offered to the owner as a new slice)"
    },
    {
      "label": "free-text",
      "summary": "keep findings as free text"
    }
  ],
  "recommendation": {
    "label": "fix-owner-later",
    "reasoning": "Owner calls reach the owner in ork instead of being decided by a fix round, and later work is filed instead of lost."
  },
  "decision": "fix-owner-later",
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
