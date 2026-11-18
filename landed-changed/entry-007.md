S387 (*proposed*): A role's safe tool feedback is filed as a slice. Each kept
tool feedback entry gets a stable `key`, and a repeat in a run is dropped.
An entry of kind `friction` or `deterministic-check` is filed with `slicer add`
and `slicer promote`, with no confirmation, in the registered project the tool
names, else in the run's own project with the tool in the title; a failure is
`file_error` on the entry and never stops the run. A `feature` entry is only
kept.
