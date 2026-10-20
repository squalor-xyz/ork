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
	AfterReply   func(method string) // after a successful handler's reply write is attempted; may be nil
	OnConnect    func()              // a client connected; may be nil
	OnDisconnect func()              // a client's connection ended; may be nil
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

func (s *Server) serveConn(ctx context.Context, nc net.Conn) {
	c := &Conn{conn: nc, enc: json.NewEncoder(nc), done: make(chan struct{})} // one compact line per message
	defer close(c.done)
	ctx = context.WithValue(ctx, connKey{}, c)
	dec := json.NewDecoder(nc)
	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return
			}
			var syn *json.SyntaxError
			var typ *json.UnmarshalTypeError
			if errors.As(err, &syn) || errors.As(err, &typ) {
				c.write(Response{Error: &Error{Code: CodeBadRequest, Message: "not a JSON request: " + err.Error()}})
			}
			return
		}
		resp := Response{ID: req.ID}
		if req.Method == "" {
			resp.Error = &Error{Code: CodeBadRequest, Message: "request has no method"}
		} else {
			resp.Result, resp.Error = s.handle(ctx, req)
		}
		err := c.write(resp)
		if resp.Error == nil && s.AfterReply != nil {
			s.AfterReply(req.Method)
		}
		if err != nil {
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

// Client is one connection to a server. Calls are serialized; Next can wait
// alongside a Call. Transport failure makes the client unusable.
type Client struct {
	mu       sync.Mutex // serializes calls
	readOnce sync.Once
	stateMu  sync.Mutex
	conn     net.Conn
	dec      *json.Decoder
	enc      *json.Encoder
	next     int64
	broken   error
	notes    []Notification
	replies  chan inbound
	wake     chan struct{}
	done     chan struct{}
}

// startReader gives Call and Next independent waits on a single decoder.
func (c *Client) startReader() {
	c.readOnce.Do(func() {
		c.replies = make(chan inbound, 1)
		c.wake = make(chan struct{}, 1)
		c.done = make(chan struct{})
		go func() {
			defer close(c.done)
			for {
				var msg inbound
				if err := c.dec.Decode(&msg); err != nil {
					c.stateMu.Lock()
					c.broken = err
					c.stateMu.Unlock()
					return
				}
				if len(msg.ID) == 0 && msg.Method != "" {
					c.stateMu.Lock()
					c.notes = append(c.notes, Notification{Method: msg.Method, Params: msg.Params})
					c.stateMu.Unlock()
					select {
					case c.wake <- struct{}{}:
					default:
					}
				} else {
					select {
					case c.replies <- msg:
					default:
						c.stateMu.Lock()
						c.broken = errors.New("ipc: unsolicited reply overflow")
						c.stateMu.Unlock()
						c.conn.Close()
						return
					}
				}
			}
		}()
	})
}
func (c *Client) readError() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.broken
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
	c.startReader()
	if err := c.readError(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.next++
	id := strconv.FormatInt(c.next, 10)
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	if err := c.enc.Encode(Request{ID: json.RawMessage(id), Method: method, Params: raw}); err != nil {
		c.conn.Close()
		return err
	}
	var msg inbound
	select {
	case msg = <-c.replies:
	case <-ctx.Done():
		c.conn.Close()
		return ctx.Err()
	case <-c.done:
		select {
		case msg = <-c.replies:
		default:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return c.readError()
		}
	}
	if string(msg.ID) != id {
		c.conn.Close()
		return fmt.Errorf("reply id %s does not match request id %s", msg.ID, id)
	}
	if msg.Error != nil {
		return msg.Error
	}
	if result != nil && len(msg.Result) > 0 {
		if err := json.Unmarshal(msg.Result, result); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Notification is a message the server sends without a request. It has no id.
type Notification struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Conn is a server-side connection, as a handler sees it. A handler that
// keeps a connection to send notifications on it takes it with ConnFrom.
type Conn struct {
	conn net.Conn
	mu   sync.Mutex // serializes writes
	enc  *json.Encoder
	done chan struct{}
}

type connKey struct{}

// ConnFrom returns the connection a request came on, or nil outside a request.
func ConnFrom(ctx context.Context) *Conn {
	c, _ := ctx.Value(connKey{}).(*Conn)
	return c
}

// writeTimeout bounds one write to a client, so a client that stops reading
// cannot hold up the daemon. The connection is closed when a write fails.
const writeTimeout = 10 * time.Second

// Notify sends a notification to the client. It fails when the connection has
// ended or the client did not read the write within writeTimeout; a failed
// write closes the connection.
func (c *Conn) Notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(Notification{Method: method, Params: raw})
}

// write sends one message as one line, under the connection's write lock.
func (c *Conn) write(v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	if err := c.enc.Encode(v); err != nil {
		c.conn.Close()
		return err
	}
	return nil
}

// Done is closed when the connection has ended.
func (c *Conn) Done() <-chan struct{} { return c.done }

// inbound is any message a server sends: a reply, or a notification.
type inbound struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// Next returns the next notification the server sent. It is for a connection
// that subscribed: a notice that came in during a Call is returned first. A
// context that ends first makes the client unusable, as a Call does.
func (c *Client) Next(ctx context.Context) (Notification, error) {
	c.startReader()
	for {
		c.stateMu.Lock()
		if len(c.notes) > 0 {
			n := c.notes[0]
			c.notes = c.notes[1:]
			c.stateMu.Unlock()
			return n, nil
		}
		err := c.broken
		c.stateMu.Unlock()
		if err != nil {
			return Notification{}, err
		}
		select {
		case <-c.wake:
		case <-c.done:
			// Drain notices received before the connection ended.
			continue
		case <-ctx.Done():
			c.conn.Close()
			return Notification{}, ctx.Err()
		}
	}
}
