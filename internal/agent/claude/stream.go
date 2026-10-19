// SPDX-License-Identifier: GPL-3.0-or-later

package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// envelope is what every stream line has.
type envelope struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Event     *struct {
		Type  string `json:"type"`
		Delta *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

type wireResult struct {
	SessionID      string  `json:"session_id"`
	Result         string  `json:"result"`
	Subtype        string  `json:"subtype"`
	IsError        bool    `json:"is_error"`
	APIErrorStatus *int    `json:"api_error_status"`
	TerminalReason string  `json:"terminal_reason"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	Usage          struct {
		InputTokens         int64 `json:"input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
		CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	} `json:"usage"`
	PermissionDenials []struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		ToolInput json.RawMessage `json:"tool_input"`
	} `json:"permission_denials"`
}

// ParseLine parses one line of `--output-format stream-json` output (or the
// single line of `--output-format json`). Lines of types the adapter does not
// read come back as EventOther.
func ParseLine(line []byte) (Event, error) {
	line = bytes.TrimSpace(line)
	var env envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Event{}, fmt.Errorf("claude: stream line: %w", err)
	}
	ev := Event{Kind: EventOther, SessionID: env.SessionID, Raw: append(json.RawMessage(nil), line...)}
	switch {
	case env.Type == "system" && env.Subtype == "init":
		ev.Kind = EventInit
	case env.Type == "rate_limit_event":
		ev.Kind = EventRateLimit
	case env.Type == "stream_event" && env.Event != nil && env.Event.Type == "content_block_delta" &&
		env.Event.Delta != nil && env.Event.Delta.Type == "text_delta":
		ev.Kind = EventTextDelta
		ev.Text = env.Event.Delta.Text
	case env.Type == "result":
		var w wireResult
		if err := json.Unmarshal(line, &w); err != nil {
			return Event{}, fmt.Errorf("claude: result event: %w", err)
		}
		r := &Result{
			SessionID:      w.SessionID,
			Text:           w.Result,
			Subtype:        w.Subtype,
			IsError:        w.IsError,
			APIErrorStatus: w.APIErrorStatus,
			TerminalReason: w.TerminalReason,
			Usage: Usage{
				InputTokens:         w.Usage.InputTokens,
				OutputTokens:        w.Usage.OutputTokens,
				CacheCreationTokens: w.Usage.CacheCreationTokens,
				CacheReadTokens:     w.Usage.CacheReadTokens,
				CostUSD:             w.TotalCostUSD,
			},
			Raw: ev.Raw,
		}
		for _, d := range w.PermissionDenials {
			r.Denials = append(r.Denials, Denial{ToolName: d.ToolName, ToolUseID: d.ToolUseID, ToolInput: d.ToolInput})
		}
		ev.Kind = EventResult
		ev.Result = r
	}
	return ev, nil
}

// streamReader is the io.Writer that stdout goes to. It keeps the raw bytes,
// parses each complete line as it arrives and tracks what Run needs.
type streamReader struct {
	raw       bytes.Buffer
	pending   []byte
	onEvent   func(Event)
	tail      []byte
	tailBytes int
	sessionID string // from the first event that has one
	result    *Result
}

func (r *streamReader) Write(p []byte) (int, error) {
	r.raw.Write(p)
	r.pending = append(r.pending, p...)
	for {
		i := bytes.IndexByte(r.pending, '\n')
		if i < 0 {
			break
		}
		r.line(r.pending[:i])
		r.pending = r.pending[i+1:]
	}
	if len(r.pending) == 0 {
		r.pending = nil // let a long line's buffer go
	}
	return len(p), nil
}

// flush handles a last line that has no newline.
func (r *streamReader) flush() {
	if len(r.pending) > 0 {
		r.line(r.pending)
		r.pending = nil
	}
}

func (r *streamReader) line(b []byte) {
	if len(bytes.TrimSpace(b)) == 0 {
		return
	}
	ev, err := ParseLine(b)
	if err != nil {
		return // not an event; the raw bytes keep it
	}
	if r.sessionID == "" {
		r.sessionID = ev.SessionID
	}
	switch ev.Kind {
	case EventTextDelta:
		r.addTail(ev.Text)
	case EventResult:
		r.result = ev.Result
	}
	if r.onEvent != nil {
		r.onEvent(ev)
	}
}

func (r *streamReader) addTail(text string) {
	r.tail = append(r.tail, text...)
	if len(r.tail) > 2*r.tailBytes {
		r.tail = append([]byte(nil), r.tail[len(r.tail)-r.tailBytes:]...)
	}
}

// Tail returns the last tailBytes bytes of streamed text, cut at a rune start.
func (r *streamReader) Tail() string {
	t := r.tail
	if len(t) > r.tailBytes {
		t = t[len(t)-r.tailBytes:]
	}
	for len(t) > 0 && !utf8.RuneStart(t[0]) {
		t = t[1:]
	}
	return string(t)
}
