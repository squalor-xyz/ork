# Driving ork from the command line

The ork tool runs slicer slices as relays across projects: a worktree, an
implementer, checks, a reviewer, a gate, then a landing. Every command takes
--json. For the relay contract itself, run slicer ai relay; this guide does
not repeat it.

## Check first

- ork doctor: slicer, adj, the vendor CLIs and each project's pin. Exit 1 means
  fix that before anything else.
- ork status --project P: runs, what needs you, open spec questions.
- A command starts the daemon when none answers. After a new ork build, a
  daemon restart is the owner's step; commands warn while the build differs.

## Launch

- ork run P ID --yes: print the plan, run the spec check, start one slice.
  Without --yes it asks Start this slice? [y/N]. Input that is not a terminal
  is never read for answers.
- ork run P ID --questions: print every open spec question and start nothing.
  Exit 3 means questions are pending.
- ork run P ID --yes --answer KEY=LABEL: answer every question and start. A
  question with no key is #N.
- ork wave start P ITEM... --yes: the same for several items; an answer is
  --answer ITEM:KEY=LABEL.
- Read the plan before you confirm. A project with no check command is
  refused, unless --allow-no-check.

## Ids

RUN is the ork- id that starts a run's row in ork status. The run- id of a
relay row is slicer's, and the commands refuse it. A wave id starts with wave-.

- ork answer, ork retry, ork fix, ork approve, ork check, ork dismiss,
  ork file, ork show take RUN.
- ork stop and ork approve take RUN or a wave- id.
- ork check RUN N marks human check N. ork file RUN N files finding N as a
  slice.
- ork wait takes RUN, several RUN, or none (every unfinished run of P).

## Steer

- ork answer RUN TEXT: answer a run paused on a question. It returns once the
  run has resumed; --wait waits until the run rests again.
- ork retry RUN [TEXT]: run the paused role or step again, with an optional
  note. It returns like answer. It also resumes a stopped run.
- ork fix RUN TEXT: send a fix note to a run at its approval card. It returns
  when the run rests again.
- ork stop RUN: stop a run. ork dismiss RUN: clear a stopped run from Needs you.
- ork show RUN: why a run is paused, its reports and checks. ork show RUN NAME
  prints one report or log.
- A pause with no question, such as lost or interrupted, takes ork retry. ork
  answer and ork fix do not work on it.
- A pause that reads "hung: no output for 30m" means ork killed a role that
  sent nothing for 30 minutes. Detected machine sleep is excluded from that
  time, so a wake alone does not cause it. ork does not retry for you. Read the
  reason, then run ork retry RUN: the same role runs again and is told the
  earlier process was lost.

## Owner-only steps

These steps are the owner's alone: ork approve (and ork check, which marks a
human check), adj answers (ork adj answer), and scope or design questions from
a spec check or a role. A run that passes the gate with no owner finding and a
green check lands by itself, unless the project turned auto-land off, so
approve is rare. Ask the owner in an interactive question. Relay the owner
pick from an interactive question only. Do not guess an answer or pipe stock
answers.

## Watch without polling

- ork wait RUN --until awaiting-approval,paused,stopped,landed --timeout 30m
  blocks until the run changes and prints it. A pending question always ends
  the wait.
- Exit codes: 0 changed, 3 question pending, 4 nothing active, 5 timeout,
  6 a role round ended (only with --stage).
- ork status shows each run's row with when its state began, as waiting
  since or updated, so a changed state shows in that line.
