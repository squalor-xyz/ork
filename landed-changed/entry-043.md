*Proposed (S314; adj 228, 229, 230):* `ork settings set --owner NAME` stores
the owner's name once for adj; `ADJ_DECIDED_BY` still wins when set. Answers
to spec questions that have a decision key are now filed in adj under that
name by default, for an option you picked (a typed answer stays in the slice
only); `ork settings set --file-answers off` turns that off. Without a name
one notice per item says `no owner set; run ork settings set --owner NAME`.
`ork settings show` and `ork doctor` show the owner and whether spec answers
are filed (run answers and ledger answers still need `ADJ_DECIDED_BY`). `projects.json` is now version 3; an older ork refuses it with a
version error, and ork still reads versions 1 and 2.
