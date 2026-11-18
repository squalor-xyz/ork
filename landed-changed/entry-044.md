*Proposed (S315; adj 231, 232):* projects can set `build_command` with
`ork projects set NAME --build CMD`. `ork daemon restart --rebuild` checks
for running roles, builds the self-host project with a ten minute timeout,
then restarts. A failed build keeps the daemon running. Status, doctor and
the self-host landed action line offer the rebuild command when configured.
`build-only-in-home-dir.sh` builds into `~/.local/bin/ork` without restarting;
the existing manual script stays. Rebuilds are always explicit.
