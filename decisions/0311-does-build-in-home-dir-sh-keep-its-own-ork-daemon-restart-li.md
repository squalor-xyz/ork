---
{
  "id": 311,
  "title": "Does `build-in-home-dir.sh` keep its own `ork daemon restart` line once it becomes the `build_command`?",
  "status": "decided",
  "key": "selfhost.rebuild-script-restart",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: Does `build-in-home-dir.sh` keep its own `ork daemon restart` line once it becomes the `build_command`?\nOptions:\n1. drop the restart line: edit the script so it only builds, matching its header comment, and `ork daemon restart --rebuild` does the restart after a good build\n2. keep the script: `--rebuild` then runs a script that restarts the daemon itself, so the busy refusal and the \"daemon not restarted\" path are bypassed\n3. separate build script: `build_command` names a new build-only script and the old script stays for manual use\nRecommend: 1 - the script's last line (`~/.local/bin/ork daemon restart || echo \"daemon not restarted ...\"`) exits 0 even when the restart is refused, so `--rebuild` could not tell the owner the daemon was not restarted. The Why also says the binary is `~/bin/ork`, but the script writes `~/.local/bin/ork`; I would take the script as the source, so the docs should say `~/.local/bin/ork`.\nDecision key: selfhost.rebuild-script-restart",
  "decision": "separate build script: `build_command` names a new build-only script and the old script stays for manual use",
  "rationale": "Answered in ork on 2026-11-11: Does `build-in-home-dir.sh` keep its own `ork daemon restart` line once it becomes the `build_command`?",
  "refs": [
    "S315"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
