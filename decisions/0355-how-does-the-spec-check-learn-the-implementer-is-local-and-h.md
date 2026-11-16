---
{
  "id": 355,
  "title": "How does the spec check learn the implementer is local, and how does the orientation pack reach the run's brief?",
  "status": "decided",
  "key": "roles.local-orientation-channel",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S365 spec check question: no run dir exists at spec-check time and SpecCheckParams carries no lineup.",
  "options": [
    {
      "label": "report-section",
      "summary": "daemon passes the items with a local planned implementer (engine.IsLocalRun on the run's lineup); the spec check report gets one '## Orientation SID' section each; ork cuts it to 300 lines/12k, saves orientation.md in the item's spec dir, copies it into the run dir at start; the claude-local implementer brief gets it as '## Orientation'"
    },
    {
      "label": "temp-file",
      "summary": "the role writes one pack file per slice in its temp dir and ork copies it; otherwise as report-section"
    },
    {
      "label": "as-written",
      "summary": "leave it to the implementer"
    }
  ],
  "recommendation": {
    "label": "report-section",
    "reasoning": "the report is the one channel ork already saves and parses; ParseSpecCheck skips unknown sections; the spec dir already holds per-item spec-check state"
  },
  "decision": "report-section",
  "rationale": "Owner chose 'Report section (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-15.",
  "refs": [
    "S365"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
