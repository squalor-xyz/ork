S246: a run's check command is slicer's `check_command` config key when it
is set, and the registry's only when slicer's is empty (adj 32). `run.plan`
gains `check_source` (`slicer`, `registry` or `none`) and `ork run` prints
`check: CMD (from slicer)`; a failed read keeps the registry's command and
adds a plan note. A run loaded again after a restart or a retry uses the same
rule. `ork doctor` checks the same command and names a slicer source in its
finding. `ork projects set --check` still writes the registry, so the
plan warning for a slicer command that cannot run names
`slicer config check_command CMD --root PATH` as the fix.
