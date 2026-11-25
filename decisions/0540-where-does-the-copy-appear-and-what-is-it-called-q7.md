---
{
  "id": 540,
  "title": "Where does the copy appear and what is it called (Q7)?",
  "status": "decided",
  "key": "matrix.copy-surface",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: Where does the copy appear and what is it called (Q7)?\nOptions:\n1. CLI and daemon: `ork roles matrix copy FROM (TO... | --all)` and the method `matrix.copy`; a TUI action is a later item on S226 or S251\n2. CLI, daemon and TUI: the same plus a TUI action in this slice (the slice grows to L and waits on S226 or S251)\n3. mode of apply: only `ork roles matrix apply --from P`, a mode of S250's command (mixes a change-set file with a copy between projects)\nRecommend: 1 - CLI and AI workflows first; the TUI action stays outside this slice, as its \"Not in this slice\" says\nDecision key: matrix.copy-surface\n\nQ6 and Q8 need no question:\n- Q6 is settled by the dependencies, which are met. S250 is done (`slicer show S250`). slicer s443 and s444 are done (`slicer show s443` and `s444` in the slicer repo). `RelayRolesApply` exists at `internal/slicercli/write.go:308`.\n- Q8 is decided by adj 258 (`process.s261-pass`): v1.2, depends on S250.\n- I could not run `slicer --version` (denied), so I did not confirm which slicer build is installed. Implement step 1 already asks the implementer to check it.",
  "decision": "CLI and daemon: `ork roles matrix copy FROM (TO... | --all)` and the method `matrix.copy`; a TUI action is a later item on S226 or S251",
  "rationale": "Answered in ork on 2026-11-25: Where does the copy appear and what is it called (Q7)? (S261)",
  "refs": [
    "S261"
  ],
  "history": [
    {
      "date": "2026-11-24",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
