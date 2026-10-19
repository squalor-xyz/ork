---
{
  "id": 66,
  "title": "May ork use a project's git HEAD as a change signal?",
  "status": "decided",
  "key": "boundary.git-head",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "git rev-parse HEAD costs 21-26 ms; pathspecs over .slicer/ come close to the tracking rule.",
  "options": [
    {
      "label": "head-only",
      "summary": "git rev-parse HEAD only"
    },
    {
      "label": "none",
      "summary": "no git change signal"
    },
    {
      "label": "pathspec",
      "summary": "git log over the store paths"
    }
  ],
  "decision": "head-only",
  "rationale": "Owner 2026-10-19 (integration review, docs/review-2026-10-19-integration.md): Only git rev-parse HEAD per project; no pathspecs over .slicer/ or decisions/.",
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
