---
{
  "id": 117,
  "title": "How does the owner clear an old stopped run from Needs you?",
  "status": "decided",
  "key": "ui.dismiss-run",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Stopped runs stay in Needs you until a newer record exists (docs/dashboard.md, ui.stopped-newer).",
  "options": [
    {
      "label": "dismissed-at",
      "summary": "A dismissed_at field in run.json on stopped runs, set by ork dismiss RUN, so ork status and the TUI agree; older orks refuse the new key"
    }
  ],
  "recommendation": {
    "label": "dismissed-at",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "dismissed-at",
  "rationale": "Owner 2026-10-20: one place both the CLI and the TUI read.",
  "refs": [
    "S34,S32"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
