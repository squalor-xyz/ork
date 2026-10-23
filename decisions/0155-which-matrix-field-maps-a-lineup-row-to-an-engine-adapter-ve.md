---
{
  "id": 155,
  "title": "Which matrix field maps a lineup row to an engine adapter: vendor or agent?",
  "status": "decided",
  "key": "engine.adapter-key",
  "decided_by": "Jon R",
  "created": "2026-10-23",
  "decided_at": "2026-10-23",
  "context": "S47 maps the matrix row's vendor field (Claude, OpenAI) to an adapter (claude, codex) through an explicit table in internal/daemon/runs.go (matrixAdapters); an unknown vendor is skipped. The matrix also has an agent field (Claude Code, Codex). The S47 reviewer marked the key choice as the owner's.",
  "options": [
    {
      "label": "vendor",
      "summary": "Map the vendor field (Claude, OpenAI) through an explicit table",
      "tradeoffs": "Matches what the adapters call (the model vendor); grok or other agents on one vendor would need a new table entry."
    },
    {
      "label": "agent",
      "summary": "Map the agent field (Claude Code, Codex)",
      "tradeoffs": "Names the harness that runs the role, which is what ork launches; a vendor served by two agents becomes expressible."
    }
  ],
  "recommendation": {
    "label": "agent",
    "reasoning": "ork launches a harness (claude, codex CLI), so the agent field names what is run; vendor stays display data."
  },
  "decision": "agent",
  "rationale": "ork launches a harness; the agent field names what is run; vendor stays display data.",
  "refs": [
    "S47",
    "S49"
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
