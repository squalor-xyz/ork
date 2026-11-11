---
{
  "id": 317,
  "title": "S344: how do the local spec check and UI pass rows stay out of normal runs?",
  "status": "open",
  "key": "roles.local-check-rows-default",
  "asked_by": "claude",
  "created": "2026-11-11",
  "context": "Owner asked 2026-11-11 to file a slice for local spec check and UI pass rows (S344).",
  "options": [
    {
      "label": "trial-only",
      "summary": "no rule names them; the S304 trial lists them first with ork projects set --spec-check-rows / --ui-pass-rows and resets them after"
    },
    {
      "label": "last-fallback",
      "summary": "a rule names them after the cloud rows, so they are a last fallback in every run"
    }
  ],
  "recommendation": {
    "label": "trial-only",
    "reasoning": "adj 296 keeps the spec check on cloud outside the S304 trial, and adj 181 bars a default local role without an owner decision."
  },
  "refs": [
    "S344"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    }
  ]
}
---
