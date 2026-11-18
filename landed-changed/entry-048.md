*Proposed (S316; adj 233, 234):* `ork approve RUN --later-checks` marks every
open human check `later`, approves, and prints a `deferred: N. TEXT` line for
each check it deferred. `ork check RUN all later` marks them without
approving. A check already `ticked` or `later` is left alone; there is no
`all ticked`. The plain `ork approve` still stops on open checks, and its
refusal names `--later-checks`.
