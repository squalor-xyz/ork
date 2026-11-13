---
{
  "id": 341,
  "title": "How does ork keep the two new local reviewer rows out of every run's reviewer and gate chains when no rule names them and the project does not list them?",
  "status": "decided",
  "key": "roles.local-check-rows-chain-exclusion",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "Question: How does ork keep the two new local reviewer rows out of every run's reviewer and gate chains when no rule names them and the project does not list them?\nOptions:\n1. local-vendor: ork leaves every reviewer row whose Vendor column is Local out of the reviewer and gate chains (it already reads that column in `isLocalRow`, `internal/daemon/modelcache.go:60`). Only the project's `spec_check_rows` and `ui_pass_rows` put such a row into the spec check or UI chain. The matrix label comes from those lists.\n2. rule-names: add a new matrix rule, not \"Spec check row (rNN)\" or \"UI pass row (rNN)\", that names the local spec check row and the local UI pass row. ork parses it and keeps both rows out of the chains always. This changes the rules and adds a parser in `internal/slicercli/write.go`.\nRecommend: 1 - the Local vendor column already marks the rows, so ork needs no new rule text or parser. It matches adj 296 (local is for implementers only outside the S304 trial). A trial that wants a local reviewer can still use an explicit `ork run` lineup override.\nDecision key: roles.local-check-rows-chain-exclusion",
  "decision": "rule-names: add a new matrix rule, not \"Spec check row (rNN)\" or \"UI pass row (rNN)\", that names the local spec check row and the local UI pass row. ork parses it and keeps both rows out of the chains always. This changes the rules and adds a parser in `internal/slicercli/write.go`.",
  "rationale": "Answered in ork on 2026-11-12: How does ork keep the two new local reviewer rows out of every run's reviewer and gate chains when no rule names them and the project does not list them? (S344)",
  "refs": [
    "S344"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
