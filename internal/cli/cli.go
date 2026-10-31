// SPDX-License-Identifier: GPL-3.0-or-later

// Package cli is ork's command line: argument parsing and output for status,
// projects, roles and doctor. Every command takes --json (cli.json). The exit
// code is 0 for success, 1 for a failure and 2 for a usage error.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/squalor-xyz/ork/internal/daemon"
	"github.com/squalor-xyz/ork/internal/ipc"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

const (
	defaultStartWait = 5 * time.Second
	defaultPollEvery = 50 * time.Millisecond
)

// codeUsage and codeDaemon are the error codes the CLI itself prints in a
// --json error object. Errors from the daemon keep the daemon's code.
const (
	codeUsage  = "usage"
	codeDaemon = "daemon_unavailable"
)

const usageText = `usage: ork COMMAND [ARGS] [--json]

  ork run P ID [--yes --well-specified y/n --split y/n]
                              [--stop-after land|commit]
                              [--implementer VENDOR:MODEL:EFFORT]
                              [--reviewer VENDOR:MODEL:EFFORT]
                              [--architect VENDOR:MODEL:EFFORT|off]
                              [--budget-minutes N] [--budget-tokens N]
                              role flags repeat to set the whole chain
                              show the plan and launch one slice
  ork wave suggest P          show slicer's suggested waves
  ork wave start P ITEM... [--yes --well-specified y/n --split y/n]
                              [--stop-after land|commit]
                              the run lineup and budget flags, for every item
                              show the plans and launch the chosen items
  ork approve RUN             finish and commit a passed slice
  ork check RUN N [ticked|later|open]
                              mark human check N of a slice that awaits approval
  ork fix RUN TEXT            send a fix note to a fix round
  ork stop RUN|WAVE           stop a slice or every unfinished run in a wave
  ork new P TITLE [--why TEXT|-] [--from ITEM] [--from-note N]
                              [--size S|M|L] [--importance N]
                              [--urgency N] [--effort N]
                              file a slice from a request or note
  ork note P ITEM [--] TEXT   add a slice note; quote TEXT; leading - is literal
                              TEXT - reads stdin; --json before P or after TEXT
  ork file RUN N              file a later finding as a slice
  ork adj answer P ID (--option LABEL | --text TEXT)
                              show the question and confirm an adj answer
  ork adj ask P TITLE [--key K] [--context TEXT|-]
                              [--option LABEL:SUMMARY]...
                              [--tradeoffs LABEL:TEXT]...
                              [--recommend LABEL] [--reasoning TEXT]
                              [--ref R]... [--tag T]...
                              ask an adj question without deciding it
  ork answer RUN TEXT         answer and resume a paused role
  ork dismiss RUN             clear a stopped run from Needs you
  ork retry RUN               retry a paused role or step
  ork status [--project P]    projects, needs-you counts and active relays
  ork projects list           list the registered projects
  ork projects add [PATH] [--name N]
                              register a project
  ork projects rm NAME        unregister a project
  ork projects pin NAME --relay N
                              pin the project's slicer relay contract at N
                              (v10 or v11) through slicer; needs no daemon
  ork projects unpin NAME     clear the pin through slicer
  ork projects set NAME [--push on|off] [--ui-doc PATH]
                              [--worktree-pattern P] [--branch-pattern P]
                              [--check CMD] [--check-minutes N] [--cap N]
                              [--budget-minutes N] [--budget-tokens N]
                              [--env-line LINE]...
                              [--permission ROLE=read-only|ROLE=]...
                              [--allow ROLE=PATTERN]...
                              [--vendor-env VENDOR:KEY=VALUE]...
                              [--unset-vendor-env VENDOR:KEY]...
                              change a project's optional fields; an empty
                              value or 0 clears one; --env-line and --allow
                              repeat to give the whole list, and one empty
                              --env-line clears it; a role or a vendor key
                              changes alone; --unset-vendor-env removes a key
                              and an empty --vendor-env value is kept; refused
                              while the project has unfinished runs
  ork settings show           show global_cap and daemon_idle_minutes, stored
                              and effective
  ork settings set [--global-cap N] [--daemon-idle-minutes N]
                              change them; 0 clears one; allowed while runs
                              are unfinished
  ork vendor [list]           list the vendors the daemon remembers as out;
                              marks happen on their own; a restart clears them
  ork vendor out NAME [--reason TEXT]
                              mark claude or codex out until back
  ork vendor back NAME        say the vendor is back; ends the mark early
  ork roles print [ROLE]      print the embedded role prompts
  ork roles matrix P          show the role matrix as slicer prints it, then
                              which rows ork uses, skips or does not run
  ork daemon run              serve the daemon in the foreground
  ork daemon stop [--force]   stop the daemon
  ork daemon restart [--force]
                              stop and start the daemon
  ork doctor [--upgrade]      check the slicer and adj versions, the vendor
                              CLIs, and the slicer contract here and for
                              each registered project with its pin; print the
                              path of this ork binary; print the commands to
                              upgrade a tool that is behind or missing
                              (slicer, adj and ork with --upgrade), and run
                              none of them

Every command takes --json. Exit codes: 0 ok, 1 failure, 2 usage.
`

// Tools names the CLIs that doctor checks, and the git that it reads a
// checkout with. An empty field means the tool's own name, found on PATH.
type Tools struct {
	Slicer, Adj, Git, Claude, Codex, Grok string
}

// App is the command line with its dependencies.
type App struct {
	Getenv  func(string) string                           // nil uses os.Getenv
	Getwd   func() (string, error)                        // current directory offer; nil uses os.Getwd
	BuildID string                                        // CLI build identity; empty means dev
	Input   io.Reader                                     // confirmation input; nil uses os.Stdin
	Call    func(context.Context, string, any, any) error // nil uses the daemon socket

	Socket    string                          // the daemon's socket
	Pid       string                          // the daemon's pid file; used only for the stop hint
	Start     func(ctx context.Context) error // starts the daemon; nil means it cannot be started
	StartWait time.Duration                   // how long to wait for the daemon after Start; zero means 5s
	PollEvery time.Duration                   // how often to try the socket in that time; zero means 50ms
	Tools     Tools
}

// Run runs the command line with the default App: the daemon's default
// socket, and no way to start a daemon. cmd/ork builds its own App with a
// Start function (S13).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	paths, err := daemon.DefaultPaths()
	if err != nil {
		fmt.Fprintf(stderr, "ork: %v\n", err)
		return ExitFailure
	}
	return (&App{Socket: paths.Socket, Pid: paths.Pid}).Run(ctx, args, stdout, stderr)
}

// output is one command's writers and output format.
type output struct {
	stdout, stderr io.Writer
	json           bool
}

// printJSON writes v as indented JSON and returns the exit code.
func (o *output) printJSON(v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return o.fail(ExitFailure, ipc.CodeInternal, err.Error())
	}
	fmt.Fprintf(o.stdout, "%s\n", b)
	return ExitOK
}

// fail prints an error and returns code. With --json the error is an object
// on stdout, as slicer prints its own; otherwise it is a line on stderr.
func (o *output) fail(code int, errCode, msg string) int {
	if o.json {
		o.printJSON(map[string]any{"error": ipc.Error{Code: errCode, Message: msg}})
		return code
	}
	fmt.Fprintf(o.stderr, "ork: %s\n", msg)
	if code == ExitUsage {
		fmt.Fprintln(o.stderr, "Run 'ork help' for usage.")
	}
	return code
}

func (o *output) usage(format string, a ...any) int {
	return o.fail(ExitUsage, codeUsage, fmt.Sprintf(format, a...))
}

// failErr reports an error from the daemon.
func (o *output) failErr(err error) int {
	e := ipc.ErrorOf(err)
	return o.fail(ExitFailure, e.Code, e.Message)
}

// parse parses a command's flags and returns its positional arguments. Flags
// and positional arguments may be mixed, so "projects add PATH --name N" works.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// Run runs one command line (without the program name) and returns the exit code.
func (a *App) Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o := &output{stdout: stdout, stderr: stderr, json: slices.Contains(args, "--json")}
	if len(args) == 0 {
		return o.usage("no command given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usageText)
		return ExitOK
	case "adj":
		return a.adj(ctx, o, rest)
	case "new":
		return a.newSlice(ctx, o, rest)
	case "note":
		return a.note(ctx, o, rest)
	case "run":
		return a.runSlice(ctx, o, rest)
	case "wave":
		return a.wave(ctx, o, rest)
	case "check", "fix":
		return a.cardCommand(ctx, o, cmd, rest)
	case "approve", "stop", "answer", "retry", "dismiss", "file":
		return a.ownerCommand(ctx, o, cmd, rest)
	case "status":
		return a.status(ctx, o, rest)
	case "projects":
		return a.projects(ctx, o, rest)
	case "settings":
		return a.settings(ctx, o, rest)
	case "vendor":
		return a.vendor(ctx, o, rest)
	case "roles":
		return a.roles(ctx, o, rest)
	case "doctor":
		return a.doctor(ctx, o, rest)
	case "daemon":
		return a.daemonCommand(ctx, o, rest)
	}
	return o.usage("unknown command %q", cmd)
}

// connect dials the daemon. When nothing answers it calls Start once and
// tries the socket again for StartWait.
func (a *App) connect(ctx context.Context) (*daemon.Client, error) {
	c, err := daemon.Dial(ctx, a.Socket)
	if err == nil {
		return c, nil
	}
	if a.Start == nil {
		return nil, fmt.Errorf("no daemon answers at %s and none can be started: %w", a.Socket, err)
	}
	if err := a.Start(ctx); err != nil {
		return nil, fmt.Errorf("start the daemon: %w", err)
	}
	wait, poll := a.StartWait, a.PollEvery
	if wait <= 0 {
		wait = defaultStartWait
	}
	if poll <= 0 {
		poll = defaultPollEvery
	}
	deadline := time.Now().Add(wait)
	for {
		c, err = daemon.Dial(ctx, a.Socket)
		if err == nil {
			return c, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return nil, fmt.Errorf("the daemon did not answer at %s within %v of starting: %w", a.Socket, wait, err)
		}
		select {
		case <-ctx.Done():
		case <-time.After(poll):
		}
	}
}

// Connect returns a connection to the daemon, starting one when nothing
// answers, as a command does. cmd/ork uses it for the TUI.
func (a *App) Connect(ctx context.Context) (*daemon.Client, error) {
	return a.connect(ctx)
}

// withDaemon connects and runs f. A connection failure is reported as such.
func (a *App) withDaemon(ctx context.Context, o *output, f func(*daemon.Client) int) int {
	c, err := a.connect(ctx)
	if err != nil {
		return o.fail(ExitFailure, codeDaemon, err.Error())
	}
	defer c.Close()
	if _, err := a.CheckDaemonBuild(ctx, c, o.stderr); err != nil {
		return o.failErr(err)
	}
	return f(c)
}
