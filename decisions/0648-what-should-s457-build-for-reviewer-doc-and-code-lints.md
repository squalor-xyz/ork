---
{
  "id": 648,
  "title": "What should S457 build for reviewer doc and code lints?",
  "status": "decided",
  "key": "review.doc-cli-output-lint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S457 spec check (S518, S536 merged).",
  "options": [
    {
      "label": "go-test-lint",
      "summary": "a go test comparing CLI output strings quoted in docs/design.md with cli_test.go expectations; S518 and S536 become separate follow-ups"
    },
    {
      "label": "ork-subcommand",
      "summary": "an ork diff-lint subcommand"
    },
    {
      "label": "retire",
      "summary": "retire"
    }
  ],
  "recommendation": {
    "label": "go-test-lint",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "go-test-lint",
  "rationale": "Owner chose 'Go test lint' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S457"
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
