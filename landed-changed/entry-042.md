*Proposed (S354; adj 325, 326):* ork accepts slicer relay v11 and v12 and
drops v10 (`boundary.contract-set`); its role prompts move to v12. Every role
may end its report with up to 3 advisory tool feedback lines. ork reads them
from the report (a malformed or surplus line is dropped, never a shape error)
and keeps up to 3 in the run record's `tool_feedback`, each with its role. They are not
findings, the gate never decides on them, and showing them in the TUI or
`ork show` is not built. A project pinned to v10 fails `ork doctor` until it
is re-pinned with `ork projects pin NAME --relay 12`.
