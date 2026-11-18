S377 (*proposed*): a landing whose base branch gains a commit after the
merge-in merges that commit in once and then fast-forwards, instead of
pausing. The same retry covers ork's own tracking commit when a landing
resumes with an older merge still open: the commit happens first, then the
settlement, so the landing merges the base in again before the
fast-forward. A base that moves again pauses at the fast-forward with the
same one-line `git merge --ff-only` reason. Docs updated: `docs/design.md`.
