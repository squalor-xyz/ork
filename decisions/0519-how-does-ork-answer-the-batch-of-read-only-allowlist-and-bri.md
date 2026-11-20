---
{
  "id": 519,
  "title": "How does ork answer the batch of read-only allowlist and brief requests from tool feedback?",
  "status": "decided",
  "key": "roles.read-only-allowlist-batch",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "Ten tool-feedback slices (S465, S469, S471, S472, S478, S479, S481, S485, S486, S488) ask read-only roles to run more commands or for the brief to state what is denied.",
  "options": [
    {
      "label": "brief-first",
      "summary": "One slice: every read-only brief states the denied forms (compound commands, shell variables, pipes, env prefixes, ; echo); the implementer brief notes that socket-bound tests may fail in its sandbox and ork's round check is the evidence; allow only plain read-only additions (slicer COMMAND --help, slicer ai relay, ls, git ls-files, rg); retire the rest"
    },
    {
      "label": "allow-all",
      "summary": "Add every requested form, including shell variables and ; echo"
    },
    {
      "label": "brief-only",
      "summary": "Brief only; no new allow rules"
    }
  ],
  "recommendation": {
    "label": "brief-first",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "brief-first",
  "rationale": "Owner chose 'One slice, brief-first (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S481"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
