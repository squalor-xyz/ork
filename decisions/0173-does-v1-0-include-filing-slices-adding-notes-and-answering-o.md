---
{
  "id": 173,
  "title": "Does v1.0 include filing slices, adding notes and answering or asking adj records from ork?",
  "status": "decided",
  "key": "scope.v1-ork-edits",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-29",
  "decided_at": "2026-10-29",
  "context": "S165 parity analysis (docs/orchestrator-parity.md) found filing slices, adding notes and answering adj records as gaps. Decided adj 1 (scope.v1) says roadmap and decision editing wait for a later version. The owner said on 2026-10-29 to make all gaps v1.0 except the matrix editor and spec drafting. Refs S170 S171 S172 S179.",
  "options": [
    {
      "label": "include",
      "summary": "v1.0 includes filing a slice (S170), adding a slice note (S171) and answering or asking adj records (S172) from ork; roadmap reorder or park (S67-S69), the matrix editor (S173) and spec drafting (S179) stay later",
      "tradeoffs": "Lets the owner run everything from ork without an orchestrating session (S165 goal); amends the written scope of adj 1 and needs ork to run slicer add/promote/note and adj ask/decide on the owner's behalf"
    },
    {
      "label": "defer",
      "summary": "All ork edits of roadmap items and decisions wait for v1.1 as scope.v1 (adj 1) says",
      "tradeoffs": "Keeps v1.0 to monitoring, launching and supervising; the owner keeps using a terminal or an outer session for filing, notes and adj answers"
    }
  ],
  "recommendation": {
    "label": "include",
    "reasoning": "Owner direction 2026-10-29: ork should handle everything the orchestrating session does so no outer session is needed; adj 11 (ui.slicer-tui) already allows common edits in ork."
  },
  "decision": "include",
  "rationale": "Owner picked include in the Claude Code session on 2026-10-29 (AskUserQuestion, v1.0 wave setup)",
  "refs": [
    "S170",
    "S171",
    "S172",
    "S179"
  ],
  "history": [
    {
      "date": "2026-10-29",
      "event": "ask"
    },
    {
      "date": "2026-10-29",
      "event": "decide"
    }
  ]
}
---
