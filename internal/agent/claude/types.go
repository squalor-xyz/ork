// SPDX-License-Identifier: GPL-3.0-or-later

// Package claude is the Claude role adapter. It builds one `claude -p`
// command, runs it in its own process group, reads the stream-json events
// as they arrive and turns the output into a Round: session id, final text,
// usage, exit class and report. It knows nothing of the engine's state
// machine and makes no call by itself; Run starts whatever Spec.Bin names.
package claude

import (
	"encoding/json"
	"strings"
	"unicode"
)

// Class is how a round ended.
type Class string

const (
	// ClassOK: exit 0 and a successful result event, including permission denials.
	ClassOK Class = "ok"
	// ClassDenied: an unsuccessful round lists permission denials.
	ClassDenied Class = "denied"
	// ClassRateLimited is reserved for fallback (vendor.fallback). No real
	// sample exists, so nothing returns it yet (vendor.ratelimit-detection,
	// vendor.unsampled-classes). Auth failures and bad models have no class
	// either; they are unclassified until sampled.
	ClassRateLimited Class = "rate-limited"
	// ClassUnclassified: any other failure. The slice stops for the owner.
	ClassUnclassified Class = "unclassified"
)

// EventKind names the stream events the adapter reads.
type EventKind string

const (
	EventInit      EventKind = "init"
	EventTextDelta EventKind = "text-delta"
	EventRateLimit EventKind = "rate-limit"
	EventResult    EventKind = "result"
	EventOther     EventKind = "other"
)

// Event is one parsed stream line.
type Event struct {
	Kind      EventKind
	SessionID string
	Text      string  // EventTextDelta: the delta
	Result    *Result // EventResult
	Raw       json.RawMessage
}

// Usage is the token and cost part of a result event.
type Usage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	CostUSD             float64
}

// Denial is one entry of permission_denials.
type Denial struct {
	ToolName  string
	ToolUseID string
	ToolInput json.RawMessage
}

// denialDetailMax is the most characters a denial's detail holds, the
// trailing "..." included (adj #105).
const denialDetailMax = 200

// Detail is the text ork records for a denied call (adj #105): the command of
// a Bash call, else the file path of a file tool, else "". It never holds file
// content. CR and LF become spaces, then all other control characters are
// removed (adj #106). Text over 200 characters (after removal) is cut to 197
// characters and "...". The cap bounds the text only: a command can hold
// secrets, and they are not removed (docs/state.md).
func (d Denial) Detail() string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(d.ToolInput, &fields); err != nil {
		return ""
	}
	text := jsonString(fields["command"])
	if text == "" {
		text = jsonString(fields["file_path"])
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.NewReplacer("\r", " ", "\n", " ").Replace(text))
	if r := []rune(text); len(r) > denialDetailMax {
		text = string(r[:denialDetailMax-3]) + "..."
	}
	return text
}

// jsonString returns the string a raw JSON value holds, or "" when it holds
// none or is absent.
func jsonString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// Result is the `result` event.
type Result struct {
	SessionID      string
	Text           string
	Subtype        string
	IsError        bool
	APIErrorStatus *int
	TerminalReason string
	Usage          Usage
	Denials        []Denial
	Raw            json.RawMessage
}

// ReportSource says where a report came from.
type ReportSource string

const (
	ReportFromFile   ReportSource = "file"
	ReportFromResult ReportSource = "result"
)

// Round is the adapter's result for one role round.
type Round struct {
	Command      []string // binary and arguments as started
	ExitCode     int      // -1 when the process was killed by a signal
	Class        Class
	Reason       string // one line saying why the class was chosen
	SessionID    string
	Text         string // the result event's text
	Usage        Usage
	Denials      []Denial
	Report       string
	ReportSource ReportSource
	Tail         string // the last Spec.TailBytes of streamed text
	Candidate    string // directory of the saved fixture candidate; empty for an ok round
	CandidateErr error  // set when saving the candidate failed
	Result       *Result
	Stdout       []byte
	Stderr       []byte
}

// Spec describes one round.
type Spec struct {
	Bin        string   // default "claude"
	Dir        string   // working directory
	Env        []string // full environment; nil inherits
	Prompt     string
	Model      string // omitted when empty
	Effort     string // omitted when empty
	ReadOnly   bool   // --permission-mode plan, no allowlist
	Allowlist  []string
	Resume     string // session id to resume
	ReportFile string // read first when non-empty
	FixtureDir string // default DefaultFixtureDir()
	TailBytes  int    // default 2048
	// OnStart reports the process group after Start and before Wait (adj #85).
	OnStart func(pgid int)
	OnEvent func(Event)
}
