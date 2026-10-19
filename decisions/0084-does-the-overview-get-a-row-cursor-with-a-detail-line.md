---
{
  "id": 84,
  "title": "Does the Overview get a row cursor with a detail line?",
  "status": "decided",
  "key": "ui.overview-cursor",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S20 cuts Overview rows to one line, but the Overview has no row selection to attach a detail line to, and cutting breaks errors-verbatim and run-ID tests.",
  "options": [
    {
      "label": "cursor",
      "summary": "row cursor in the Overview with a detail line for the selected row; rows cut with an ellipsis, run IDs whole"
    },
    {
      "label": "cut-only",
      "summary": "cut with no detail line"
    },
    {
      "label": "wrap",
      "summary": "keep wrapping error and run rows"
    }
  ],
  "decision": "cursor",
  "rationale": "Owner 2026-10-19: row cursor with a detail line; rows cut with an ellipsis, run IDs stay whole.",
  "refs": [
    "S20"
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
