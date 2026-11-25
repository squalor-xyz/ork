---
{
  "id": 563,
  "title": "How should S560 be specified before an implementer starts?",
  "status": "decided",
  "key": "relay.landed-changed-path-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: How should S560 be specified before an implementer starts?\nOptions:\n1. rule: a deterministic engine step that runs after the implementer hands off and before the reviewer starts. It scans the run's decisions (adj records the slice cites, plus the owner answers recorded for the run) for `landed-changed/<ID>.md` paths. Any path not in the branch diff against the base is added to the reviewer's brief as a flagged finding. It does not block the run.\n2. gate: the same scan, but a missing path blocks the handoff, so the implementer must fix it before review starts.\n3. docs only: put the check in the reviewer role prompt as an instruction and write no code. This does not meet the \"deterministic by code\" rule in the global instructions.\nRecommend: 1 - the reported case, an owner decision naming S548.md while the diff touched only S558.md, is a mismatch that is worth showing to the reviewer. Blocking could wrongly stop a slice whose decision names a path for a different slice. The owner picks the option, then the spec still needs its Files, Failing tests, Implement and Check sections written.\nDecision key: relay.landed-changed-path-check\n\nFindings behind the question:\n- `slicer show S560 --json --lean` returns only the **Why** body. The Files, Failing tests, Implement, Check and Git sections have no body, and the slice has no Not-in-this-slice text. There are no named files, no named tests with assertions, no steps and no check command.\n- The slice leaves open which text counts as an \"owner decision\": adj records, the spec's own sections, or the run's recorded answers. It also leaves open how a flag is raised and whether it blocks.\n- I could not close these by reading the code. `/home/jon/code/squalor-xyz/ork/internal/engine/changelog.go:18` defines `defaultEntriesDir = \"landed-changed\"` and handles the changelog entry itself, but it has no scan of decision text.\n- `/home/jon/code/squalor-xyz/ork/docs/design.md:725` mentions a check command that runs once, after a green round check and before review. That is a possible place to hook in, but the slice does not say to use it.\n- `adj check` and `adj list --tag review` returned nothing that settles this. I found no existing decided record or key for the topic, so the key above is new.",
  "decision": "rule: a deterministic engine step that runs after the implementer hands off and before the reviewer starts. It scans the run's decisions (adj records the slice cites, plus the owner answers recorded for the run) for `landed-changed/<ID>.md` paths. Any path not in the branch diff against the base is added to the reviewer's brief as a flagged finding. It does not block the run.",
  "rationale": "Answered in ork on 2026-11-25: How should S560 be specified before an implementer starts? (S560)",
  "refs": [
    "S560"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
