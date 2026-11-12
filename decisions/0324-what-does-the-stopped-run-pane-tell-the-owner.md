---
{
  "id": 324,
  "title": "What does the stopped run pane tell the owner?",
  "status": "decided",
  "key": "ui.stopped-pane-resume",
  "asked_by": "Claude",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S296 UI pass: the stopped pane said 'Stopped; relaunch, or clear with: ork dismiss ID' and hid the new ork retry resume path; TestPaneStoppedReadOnly pinned no ork retry.",
  "options": [
    {
      "label": "fix-now",
      "summary": "Stopped; resume with: ork retry ID [--rounds N], or clear with: ork dismiss ID, retry part only when resumable; in S296"
    },
    {
      "label": "text-only-now",
      "summary": "Replace relaunch with resume with ork retry ID in the footer and doc only"
    },
    {
      "label": "later",
      "summary": "Follow-up slice"
    }
  ],
  "recommendation": {
    "label": "text-only-now",
    "reasoning": "One-line copy fix that keeps the pane read-only."
  },
  "decision": "fix-now",
  "rationale": "Owner 2026-11-11, interactive question in the ork session (S296 UI finding): Fix now, full.",
  "refs": [
    "S296"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
