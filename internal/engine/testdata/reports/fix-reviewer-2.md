Synthetic report written for the S381 tests. Review, round 1.

## (A) Correctness

nothing

I traced the stop path and it releases the lock.

## (B) Test gaps

`TestGateStops` never reaches the stop: it returns at the first state check, so the cancel line is not covered.

Add a case that cancels while the gate runs.

## (C) Direction

nothing

## (D) Doc accuracy

nothing

## Owner needed

none
