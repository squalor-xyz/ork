---
{
  "id": 150,
  "title": "What report shape does the engine check for the architect role?",
  "status": "decided",
  "key": "slice.architect-report",
  "decided_by": "Jon R",
  "created": "2026-10-23",
  "decided_at": "2026-10-23",
  "context": "S08 adds an architect round. CheckReport (internal/engine/report.go:37-84) has branches for the gate (VERDICT line, Tests line) and the implementer, and every role needs an Owner needed section. The architect prompt (internal/roles/prompts/architect.md) asks for change-now and later findings, each marked CONFIRMED or PLAUSIBLE, with no fixed headings the engine could parse.",
  "options": [
    {
      "label": "owner-section",
      "summary": "Check only the shared Owner needed section; the engine treats the whole report as findings text for the implementer fix round",
      "tradeoffs": "Smallest change, no new parsing; the engine cannot tell a clean architect report from one with findings, so every architect report goes back to the implementer as context."
    },
    {
      "label": "findings-heading",
      "summary": "Require a Change now heading (none or a list) and parse it",
      "tradeoffs": "The engine can skip the fix round when Change now is none; the prompt must change and the relay contract text is not ork's to change, so ork's prompt would drift from other relay tools."
    }
  ],
  "recommendation": {
    "label": "owner-section",
    "reasoning": "Keeps ork's architect report the same as the contract's free-form report and adds no parser; revisit if fix rounds from empty architect reports prove costly."
  },
  "decision": "owner-section",
  "rationale": "Matches the contract's free-form architect report and adds no parser.",
  "refs": [
    "S08"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    },
    {
      "date": "2026-10-23",
      "event": "decide"
    }
  ]
}
---
