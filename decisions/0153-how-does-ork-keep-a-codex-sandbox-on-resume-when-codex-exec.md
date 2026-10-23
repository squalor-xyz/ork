---
{
  "id": 153,
  "title": "How does ork keep a codex sandbox on resume, when codex exec resume refuses -s?",
  "status": "decided",
  "key": "vendor.codex-resume-sandbox",
  "asked_by": "implementer",
  "decided_by": "Jon R",
  "created": "2026-10-23",
  "decided_at": "2026-10-23",
  "context": "S48 probe 06 (testdata/vendors/codex/06-resume-workspace-write.stderr, codex-cli 0.164.0): 'error: unexpected argument -s found', exit 2, no API call. Resume help lists -c but no -s or -C. Whether a resumed session keeps its first-run sandbox, or whether -c sandbox_mode=... works on resume, is unmeasured. Each answer to measure costs one paid codex call.",
  "options": [
    {
      "label": "fresh",
      "summary": "Never resume codex roles; start a fresh session each round",
      "tradeoffs": "No sandbox gap, but loses session cache and context."
    },
    {
      "label": "measure",
      "summary": "Spend one paid call to test resume with -c sandbox_mode and see what a bare resume inherits",
      "tradeoffs": "Costs one call; may show resume is fine."
    }
  ],
  "recommendation": {
    "label": "measure",
    "reasoning": "Resume is in the design table; one call answers whether it is usable for sandboxed roles."
  },
  "decision": "measure",
  "rationale": "Owner 2026-10-23: S47 needs resume for write roles; one paid call measures it.",
  "refs": [
    "S48",
    "S01"
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
