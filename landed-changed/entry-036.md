S361: keep claude-local progress notes in `RUNDIR/tmp/progress-ROLE.md`,
without creating project `.scratch/` directories. Allow simple read-only
commands on the role's own temp files, report and progress note without
an owner question; read-only roles retain their exact-note write grant.
