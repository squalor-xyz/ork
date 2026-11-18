S371: ork keeps the read log of a claude-local role. Its approval point
appends each allowed Read (`path:start-end`) and Grep (`pattern in path`) to
`RUNDIR/tmp/readlog-ROLE-ROUND.txt`, and a fresh session's brief carries it
under `## Already read` after the progress note, merged, newest last, at most
80 lines, with "do not read them again unless you are about to edit that
range" (adj roles.local-read-log-content, roles.local-read-log-reuse). Cloud
roles are unchanged.
