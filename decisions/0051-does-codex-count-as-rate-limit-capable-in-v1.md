---
{
  "id": 51,
  "title": "Does codex count as rate-limit-capable in v1?",
  "status": "decided",
  "key": "vendor.codex-ratelimit",
  "asked_by": "s01 implementer",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "vendor.ratelimit-detection is decided: classify only documented signals; anything unclassified stops the slice for the owner (adj show 23). The codex CLI help (codex --help and codex exec --help) documents no rate-limit or quota signal, and the spike forced none. A bad model gave an error item and turn.failed in JSONL (testdata/vendors/codex/04-bad-model.stdout); that is not a rate limit. So a codex rate limit cannot be classified in v1, and automatic fallback from codex is not yet supported by any documented signal.",
  "options": [
    {
      "label": "stop-for-owner",
      "summary": "codex stays in fallback chains; a codex rate-limit exit stops the slice for the owner until a real capture is classified",
      "tradeoffs": "matches the decided rule with no new classifier; a codex limit costs an owner stop"
    },
    {
      "label": "no-fallback-from-codex",
      "summary": "codex is never a fallback source; a codex limit stops the slice, and codex may still be a fallback target",
      "tradeoffs": "narrower than stop-for-owner; still needs the same stop rule"
    },
    {
      "label": "read-docs-first",
      "summary": "look up codex's documented rate-limit signal in its online docs before v1, then decide",
      "tradeoffs": "delays the decision; the spike read only the local CLI help"
    }
  ],
  "recommendation": {
    "label": "stop-for-owner",
    "reasoning": "It follows the decided rule without a new classifier, and codex stays usable as a fallback target. The read-docs option may still add a documented signal later."
  },
  "decision": "Match the reported strings: ork classifies a codex turn.failed or error whose message contains \"You've hit your usage limit\" or an HTTP 429 with rate_limit_exceeded as rate-limited and falls back automatically. This amends vendor.ratelimit-detection (documented signals only) for codex.",
  "rationale": "Owner 2026-10-19 after reading Codex docs: the official non-interactive docs (learn.chatgpt.com/docs/non-interactive-mode) name error and turn.failed events with no rate-limit fields or exit codes; the strings come from user reports (OpenAI community forum; github.com/openai/codex/issues/9135). Known risk: the usage-limit message has been reported while quota remained, so a false fallback is possible.",
  "refs": [
    "S01"
  ],
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
