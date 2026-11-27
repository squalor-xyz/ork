---
{
  "id": 663,
  "title": "What form do per-test revert commands take, and where does the vet sample come from?",
  "status": "decided",
  "key": "base-overlays.per-test-and-vet-sample",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S643 spec check.",
  "options": [
    {
      "label": "per-test-live",
      "summary": "one go test -overlay -count=1 -run '^TestName$' line per test in the changed test files; quote the first line of a real vet failure from this brief's overlays, omitted when none fails"
    },
    {
      "label": "fixed-sample",
      "summary": "fixed sample"
    },
    {
      "label": "fail-only",
      "summary": "FAIL-only filter"
    }
  ],
  "recommendation": {
    "label": "per-test-live",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "per-test-live",
  "rationale": "Owner chose 'Per-test lines + live sample' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S643"
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
