---
{
  "id": 78,
  "title": "How are fixture candidates saved, and for which rounds?",
  "status": "decided",
  "key": "vendor.fixture-candidates",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "vendor.ratelimit-detection says capture raw output for every non-ok exit; docs/state.md proposed redacting before saving.",
  "options": [
    {
      "label": "raw-0600",
      "summary": "Raw at 0600, redact on promote, denials included"
    },
    {
      "label": "redact-first",
      "summary": "Redact before saving"
    },
    {
      "label": "skip-denials",
      "summary": "Raw, but skip denials"
    }
  ],
  "decision": "raw-0600",
  "rationale": "Owner 2026-10-19: raw at mode 0600 under the state dir, never in a repo, for every non-ok round including permission denials that exit 0; redact when a candidate is promoted into testdata/.",
  "refs": [
    "S16"
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
