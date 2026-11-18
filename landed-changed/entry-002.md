S402 (*proposed*, adj 405): a merged run removes its item's spec-check
directory only when the run has a commit time and no file in the check is
newer than that commit. A restart or a git error no longer removes a later spec check
of the same item. A run with no commit time removes nothing.
