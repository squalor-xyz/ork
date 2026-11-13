---
{
  "id": 342,
  "title": "S344: how do the local spec check and UI pass rows stay out of normal runs? (narrows 317)",
  "status": "decided",
  "key": "roles.local-check-rows-default",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "chat, ork relay session 8d711f67",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "Owner picked rule-names for S344's spec question (adj 341), which contradicts the 'no rule names them' half of 317 trial-only. Owner asked 2026-11-12 to supersede 317.",
  "options": [
    {
      "label": "trial-list-and-rule",
      "summary": "the S304 trial lists the rows first with ork projects set --spec-check-rows / --ui-pass-rows and resets them after; a classifying matrix rule (not a fallback) names them so ork keeps them out of the reviewer, gate and UI chains (adj 341)"
    },
    {
      "label": "last-fallback",
      "summary": "a rule names them after the cloud rows, so they are a last fallback in every run"
    }
  ],
  "recommendation": {
    "label": "trial-list-and-rule",
    "reasoning": "Keeps 317's trial-only listing and takes the owner's rule-names answer (341) in place of 317's 'no rule names them'."
  },
  "decision": "trial-list-and-rule",
  "rationale": "Owner, 2026-11-12, ork relay session 8d711f67, chat: 'yes supersede 317.' after picking rule-names for S344 (adj 341); the trial-only listing of 317 stays.",
  "supersedes": [
    317
  ],
  "refs": [
    "S344"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    },
    {
      "date": "2026-11-12",
      "event": "supersede",
      "note": "supersedes 317"
    }
  ]
}
---
