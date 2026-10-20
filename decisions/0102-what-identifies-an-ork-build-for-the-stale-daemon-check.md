---
{
  "id": 102,
  "title": "What identifies an ork build for the stale-daemon check?",
  "status": "decided",
  "key": "daemon.build-id",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "cmd/ork/main.go:29 has var version = dev, set only by -ldflags; go install and ~/bin/ork builds leave it as dev, so a version compare would miss a stale daemon.",
  "options": [
    {
      "label": "vcs",
      "summary": "vcs.revision plus vcs.modified from runtime/debug.ReadBuildInfo, falling back to the version var",
      "tradeoffs": "stdlib; set by go build and install in a git checkout; absent outside one"
    },
    {
      "label": "ldflags",
      "summary": "compare the version var only",
      "tradeoffs": "every build must pass -ldflags"
    },
    {
      "label": "hash",
      "summary": "hash the running binary",
      "tradeoffs": "catches any rebuild; reads the whole file; hard to show a person"
    }
  ],
  "recommendation": {
    "label": "vcs",
    "reasoning": "Stdlib, automatic for the owner's go install and go build."
  },
  "decision": "vcs",
  "rationale": "Owner 2026-10-20: VCS revision from build info.",
  "refs": [
    "S46"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
