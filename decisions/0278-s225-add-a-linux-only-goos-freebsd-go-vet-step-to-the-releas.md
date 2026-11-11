---
{
  "id": 278,
  "title": "S225: add a Linux-only GOOS=freebsd go vet step to the release workflow's test job?",
  "status": "decided",
  "key": "ci.cross-goos-vet",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S225 spec check 2026-11-11: the spec defaults to the step, but the 2026-11-01 note lists it as open and no adj record existed.",
  "options": [
    {
      "label": "add-step",
      "summary": "add the Linux-only 'Vet another unix target' step and its cross_vet guard in workflow_test.go"
    },
    {
      "label": "helper-only",
      "summary": "fix the getsid helper by rename; no release.yml or workflow_test.go edits"
    }
  ],
  "recommendation": {
    "label": "add-step",
    "reasoning": "One extra vet run on the Linux job stops the regression."
  },
  "decision": "add-step",
  "rationale": "Owner picked Add the CI step (recommended) in an interactive question on 2026-11-11.",
  "refs": [
    "S225"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
