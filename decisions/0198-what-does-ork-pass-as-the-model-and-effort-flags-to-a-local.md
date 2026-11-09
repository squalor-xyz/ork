---
{
  "id": 198,
  "title": "What does ork pass as the model and effort flags to a `-local` launcher?",
  "status": "decided",
  "key": "roles.local-model.model-flags",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Question: What does ork pass as the model and effort flags to a `-local` launcher?\nOptions:\n1. launcher-owns-model: for the three local vendors ork passes no `--model`, `-m` or `--effort`, and the row's Model and Reasoning fields are informational only. This avoids `grok-local`'s own `-m studio-local` clashing with the row's `-m qwen3.8-27b` (`internal/agent/grok/grok.go:66`).\n2. pass-row-values: ork passes the row's model and effort as for cloud rows. If step 1's evidence shows a clash or an error, the implementer stops and asks.\n3. per-launcher: decide per launcher after step 1's evidence, which means the implementer pauses for you\nRecommend: 1 - adj 183 and adj 188 make the launcher script own the environment and model, and step 2 of Implement already names the `grok-local` clash without saying how to resolve it.\nDecision key: roles.local-model.model-flags",
  "decision": "pass-row-values: ork passes the row's model and effort as for cloud rows. If step 1's evidence shows a clash or an error, the implementer stops and asks.",
  "rationale": "Answered in ork on 2026-11-09: What does ork pass as the model and effort flags to a `-local` launcher?",
  "refs": [
    "S301"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
