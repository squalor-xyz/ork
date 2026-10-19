---
{
  "id": 68,
  "title": "Does ork doctor fail when a vendor CLI is missing?",
  "status": "decided",
  "key": "doctor.vendors-optional",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S03 reports a missing or broken vendor CLI without failing; a lineup needs only the vendors it uses.",
  "options": [
    {
      "label": "report-only",
      "summary": "Report it and pass"
    },
    {
      "label": "fail",
      "summary": "Fail doctor when any vendor is missing"
    }
  ],
  "decision": "report-only",
  "rationale": "Owner 2026-10-19: confirmed S03's behavior: report a missing vendor, do not fail.",
  "refs": [
    "S03"
  ],
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
