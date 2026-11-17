Synthetic report written for the S381 tests. Review, round 1.

## (A) Correctness, most severe first

1. `internal/engine/report.go:530` drops the error from `splitToolFeedback`, so a bad report reads as clean.

## (B) Test gaps

nothing

I read the new tests and each one fails when its product line is removed.

## (C) Direction

Fits the design.

## (D) Doc accuracy

nothing

## Owner needed

none
