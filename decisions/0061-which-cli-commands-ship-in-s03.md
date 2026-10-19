---
{
  "id": 61,
  "title": "Which CLI commands ship in S03?",
  "status": "decided",
  "key": "cli.s03-commands",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Wave, approve, answer and stop need the engine (S04, S05).",
  "options": [
    {
      "label": "status-projects-roles-doctor",
      "summary": "four commands, doctor makes no paid calls"
    },
    {
      "label": "doctor-logs-in",
      "summary": "doctor also makes one call per vendor"
    },
    {
      "label": "minimal",
      "summary": "status and roles print only"
    }
  ],
  "decision": "status-projects-roles-doctor",
  "rationale": "Owner 2026-10-19: ork status, ork projects add|list|rm, ork roles print, and ork doctor with no paid calls (CLIs present, versions, contract pin); the vendor login check comes in S06.",
  "refs": [
    "S03"
  ],
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
