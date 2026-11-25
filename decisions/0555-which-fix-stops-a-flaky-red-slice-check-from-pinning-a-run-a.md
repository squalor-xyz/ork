---
{
  "id": 555,
  "title": "Which fix stops a flaky red slice check from pinning a run across retries?",
  "status": "decided",
  "key": "checks.red-evidence-retry",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Which fix stops a flaky red slice check from pinning a run across retries?\nOptions:\n1. rerun-on-retry: `ork retry` always re-runs red slice checks (force=true on the retry path in `runSliceChecks`, internal/engine/states.go:1061). Green batches stay cached, and red is never reused after a retry grant.\n2. red-only: a red batch is never reused. Any round that finds a red cached batch re-runs it, and green batches are still reused while product and Check text are unchanged.\n3. recheck-flag: add an `ork retry --recheck` flag that forces a fresh run. A plain retry still reuses red evidence.\nRecommend: 1 - a retry is the owner's explicit grant to try again. It needs no new flag or cache rule, and the \"product unchanged\" reuse still applies outside a retry. Option 2 would make every red round re-run the full suite.\nDecision key: checks.red-evidence-retry",
  "decision": "rerun-on-retry: `ork retry` always re-runs red slice checks (force=true on the retry path in `runSliceChecks`, internal/engine/states.go:1061). Green batches stay cached, and red is never reused after a retry grant.",
  "rationale": "Answered in ork on 2026-11-25: Which fix stops a flaky red slice check from pinning a run across retries? (S545)",
  "refs": [
    "S545"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
