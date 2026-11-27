---
{
  "id": 633,
  "title": "Which merged pieces does S594 carry, and how does the test-file revert work?",
  "status": "decided",
  "key": "baseoverlay.test-file-revert",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S594 spec check after the 2026-11-27 triage merge.",
  "options": [
    {
      "label": "closure-gate-revert-named",
      "summary": "extend the named-file closure to _test.go files vet names (a new test file maps to empty; the entry says those tests do not run), and build a fresh Base overlays section for the gate's fix round (S637); S640 and the vet sample go to a new slice"
    },
    {
      "label": "all-four",
      "summary": "all four pieces"
    },
    {
      "label": "closure-only",
      "summary": "closure only"
    }
  ],
  "recommendation": {
    "label": "closure-gate-revert-named",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "closure-gate-revert-named",
  "rationale": "Owner chose 'Closure + gate, revert named tests' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S594"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
