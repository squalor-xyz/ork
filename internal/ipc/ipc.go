// SPDX-License-Identifier: GPL-3.0-or-later

// Package ipc is ork's local socket protocol: one JSON message per line over a
// Unix socket. A client sends {"id","method","params"} and the server answers
// {"id","result"} or {"id","error":{"code","message"}}. The id is echoed
// exactly as the client sent it. Requests on one connection are answered in
// order.
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// Request is one client message.
type Request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the server's answer to one request. Exactly one of Result and
// Error is set.
type Response struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// Error is a failed request, as it travels over the socket. A slicer or adj
// tool error keeps its code and message.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("%s (%s)", e.Message, e.Code) }

// Codes the protocol itself uses. Handlers add their own.
const (
	CodeBadRequest = "bad_request"
	CodeInternal   = "internal"
)

// ErrorOf converts an error to its wire form: an *Error in the chain is used
// as it is, a slicer or adj tool error keeps its code and message, and
// anything else becomes code "internal". A nil error gives nil.
func ErrorOf(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	var te *clirun.ToolError
	if errors.As(err, &te) {
		return &Error{Code: te.Code, Message: te.Message}
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}

// Handler answers one request. Its result is encoded as JSON.
type Handler func(ctx context.Context, method string, params json.RawMessage) (any, error)

// Server serves requests on a listener.
type Server struct {
	Handler      Handler
	OnConnect    func() // a client connected; may be nil
	OnDisconnect func() // a client's connection ended; may be nil
}

// MkdirPrivate creates dir, and its parents, and sets dir to mode 0700, also
// when it already existed with a wider mode. It is for ork's own directories.
func MkdirPrivate(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

// MaxSocketPath is the longest socket path that works on every supported
// system: macOS limits sun_path to 104 bytes, one of them the terminating NUL.
const MaxSocketPath = 103

// Listen listens on the Unix socket at path. It makes the socket's directory
// mode 0700 and sets the socket to mode 0600. It refuses a path longer than
// MaxSocketPath. It never removes an existing file: that is the caller's
// decision.
func Listen(path string) (net.Listener, error) {
	if len(path) > MaxSocketPath {
		return nil, fmt.Errorf("socket path %s is %d bytes; macOS limits a socket path to 104 bytes including the terminating NUL", path, len(path))
	}
	if err := MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

// Serve answers requests until ctx ends or the listener is closed. A failed
// Accept is retried with a back-off of up to a second. It closes the
// listener and every connection, and returns once all handlers have finished.
// It returns nil when ctx ended.
func (s *Server) Serve(ctx context.Context, l net.Listener) error {
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		conns  = map[net.Conn]struct{}{}
		closed bool
	)
	closeAll := func() {
		l.Close()
		mu.Lock()
		closed = true
		for c := range conns {
			c.Close()
		}
		mu.Unlock()
	}
	stop := context.AfterFunc(ctx, closeAll)
	defer stop()
	var delay time.Duration // the back-off after failed accepts

	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				// A failed accept, such as running out of file descriptors,
				// must not end the daemon: back off and try again, as
				// net/http does.
				if delay == 0 {
					delay = 5 * time.Millisecond
				} else if delay *= 2; delay > time.Second {
					delay = time.Second
				}
				select {
				case <-ctx.Done():
				case <-time.After(delay):
				}
				continue
			}
			closeAll()
			wg.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		delay = 0
		mu.Lock()
		if closed {
			mu.Unlock()
			conn.Close()
			continue
		}
		conns[conn] = struct{}{}
		mu.Unlock()
		if s.OnConnect != nil {
			s.OnConnect()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.serveConn(ctx, conn)
			conn.Close()
			mu.Lock()
			delete(conns, conn)
			mu.Unlock()
			if s.OnDisconnect != nil {
				s.OnDisconnect()
			}
		}()
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn) // one compact line per message
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			var syn *json.SyntaxError
			var typ *json.UnmarshalTypeError
			if errors.As(err, &syn) || errors.As(err, &typ) {
				enc.Encode(Response{Error: &Error{Code: CodeBadRequest, Message: "not a JSON request: " + err.Error()}})
			}
			return
		}
		resp := Response{ID: req.ID}
		if req.Method == "" {
			resp.Error = &Error{Code: CodeBadRequest, Message: "request has no method"}
		} else {
			resp.Result, resp.Error = s.handle(ctx, req)
		}
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func (s *Server) handle(ctx context.Context, req Request) (result json.RawMessage, werr *Error) {
	defer func() {
		if r := recover(); r != nil {
			result, werr = nil, &Error{Code: CodeInternal, Message: fmt.Sprintf("handler panic: %v", r)}
		}
	}()
	v, err := s.Handler(ctx, req.Method, req.Params)
	if err != nil {
		return nil, ErrorOf(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, ErrorOf(fmt.Errorf("encode result: %w", err))
	}
	return b, nil
}

// Client is one connection to a server. Calls are serialized. After a call
// fails for any reason but a remote *Error, the client is unusable.
type Client struct {
	mu     sync.Mutex
	conn   net.Conn
	dec    *json.Decoder
	enc    *json.Encoder
	next   int64
	broken error
}

// Dial connects to the socket at path.
func Dial(ctx context.Context, path string) (*Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, dec: json.NewDecoder(conn), enc: json.NewEncoder(conn)}, nil
}

// Call sends a request and decodes the result into result (nil to ignore it).
// A failed request returns the server's *Error.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken != nil {
		return c.broken
	}
	fail := func(err error) error {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.broken = err
		return err
	}

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		raw = b
	}
	c.next++
	id := strconv.FormatInt(c.next, 10)

	// A cancelled or expired ctx interrupts the blocked read or write by
	// moving the connection's deadline to now.
	c.conn.SetDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()) })
	defer stop()

	if err := c.enc.Encode(Request{ID: json.RawMessage(id), Method: method, Params: raw}); err != nil {
		return fail(err)
	}
	var resp Response
	if err := c.dec.Decode(&resp); err != nil {
		return fail(err)
	}
	if string(resp.ID) != id {
		return fail(fmt.Errorf("reply id %s does not match request id %s", resp.ID, id))
	}
	if resp.Error != nil {
		return resp.Error
	}
	if result != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }
