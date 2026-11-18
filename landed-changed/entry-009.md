S379 (*proposed*): The spec check skips a vendor marked out. A spec check row
on an out vendor is left out of the chain, so the next row runs; a spec-check
role that exits rate-limited or out of credits marks its vendor out and the
check moves to the next row. With every row out, the check is refused with
code `refused`, naming the vendors and when each one ends.
