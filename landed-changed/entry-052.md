S408 (adj 458, adj 459): `ork retry RUN` re-lands a committed run that
stopped in land. It merges the base branch again, re-runs the landing
checks and lands when they are green. The land fix count resets, so a red
re-check gets one implementer fix round and then stops with `land blocked`
if it is still red. `--rounds` is refused. Any other committed run, or one
whose worktree or branch is gone, stays refused. `ork run` does not resume
it.
