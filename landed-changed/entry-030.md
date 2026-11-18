*Proposed (S311):* store and clear per-project `notify_command`
with `ork projects set NAME --notify CMD`, including while runs are
unfinished. Execute the whole value as one program, without arguments,
on question, approval, pause, stop and landing events. Event data arrives
in ORK environment variables. Delivery has a ten-second timeout and a
persisted per-occurrence claim (adj 359), never blocks a run, and logs
failures without retrying. `ork projects list` and `ork doctor` display
it; doctor warns about unresolved executables. The `failed` event waits
for a ledger read.
