S222 (*proposed*): Approve a wave in order. `ork approve WAVE-ID`, the daemon
method `wave.approve` and the `A` key (Overview row or run pane, after a
`y/N` that names the runs) approve every run of a wave that waits for
approval, one after the other in start order, each only after the previous run
rests again, and stop at the first run that does not end `committed` (a
landing that goes to a fix round, a pause or an error); the rest are reported
`not-attempted`. A run set to stop after its commit ends `committed` without
landing, counts as done and the wave goes on. The call never ticks a human check: with any check
open on a waiting run it is refused whole and approves nothing. The engine
run rows carry `wave` (omitted when empty).
