// SPDX-License-Identifier: GPL-3.0-or-later

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/squalor-xyz/ork/internal/clirun"
)

// shortDir returns a directory whose socket path fits macOS's 104 byte limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ork")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if len(filepath.Join(dir, "d", "ork.sock")) > 100 {
		dir = t.TempDir()
		if len(filepath.Join(dir, "d", "ork.sock")) > 100 {
			t.Skipf("no temp directory short enough for a socket: %s", dir)
		}
	}
	return dir
}

// serve starts a server with the handler on a fresh socket and returns the
// socket path. The server stops when the test ends.
func serve(t *testing.T, s *Server) string {
	t.Helper()
	path := filepath.Join(shortDir(t), "d", "ork.sock")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	})
	return path
}

func echo(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "echo":
		return params, nil
	case "fail":
		return nil, &Error{Code: "custom", Message: "custom failure"}
	case "plain":
		return nil, errors.New("something broke")
	}
	return nil, &Error{Code: "unknown_method", Message: "unknown method " + method}
}

func rawExchange(t *testing.T, path, line string) (string, net.Conn) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if _, err := conn.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("no reply line: %v", err)
	}
	return reply, conn
}

func TestRequestGetsResultWithItsId(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	reply, _ := rawExchange(t, path, `{"id":7,"method":"echo","params":{"a":1}}`+"\n")

	if strings.Count(reply, "\n") != 1 {
		t.Fatalf("reply is not one line: %q", reply)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(reply), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["id"]) != "7" || string(got["result"]) != `{"a":1}` {
		t.Fatalf("reply: %s", reply)
	}
	if _, has := got["error"]; has {
		t.Fatalf("a result reply carries an error: %s", reply)
	}
}

func TestFailedRequestGetsCodeAndMessage(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	reply, _ := rawExchange(t, path, `{"id":"abc","method":"fail"}`+"\n")

	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(reply), &got); err != nil {
		t.Fatal(err)
	}
	if string(got["id"]) != `"abc"` || string(got["error"]) != `{"code":"custom","message":"custom failure"}` {
		t.Fatalf("reply: %s", reply)
	}
	if _, has := got["result"]; has {
		t.Fatalf("an error reply carries a result: %s", reply)
	}
}

func TestClientCallsInSequence(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for i := 0; i < 3; i++ {
		var out struct{ N int }
		if err := c.Call(context.Background(), "echo", map[string]int{"N": i}, &out); err != nil || out.N != i {
			t.Fatalf("call %d: %+v, %v", i, out, err)
		}
	}
	var e *Error
	err = c.Call(context.Background(), "fail", nil, nil)
	if !errors.As(err, &e) || e.Code != "custom" || e.Message != "custom failure" {
		t.Fatalf("remote error: %T %v", err, err)
	}
	// The connection still works after a remote error.
	if err := c.Call(context.Background(), "echo", map[string]int{"N": 9}, nil); err != nil {
		t.Fatalf("call after a remote error: %v", err)
	}
}

func TestToolErrorKeepsCodeAndMessageAcrossTheSocket(t *testing.T) {
	for _, tc := range []struct{ tool, fixture, code, message string }{
		{"slicer", filepath.Join("..", "..", "testdata", "cli", "slicer", "error-no-such-item.json"), "no_such_item", "no such item: NOPE"},
		{"adj", "", "no_such_record", "no record with key nope.x"},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			stdout := `{"error":{"code":"` + tc.code + `","message":"` + tc.message + `","command":"show"}}`
			if tc.fixture != "" {
				b, err := os.ReadFile(tc.fixture)
				if err != nil {
					t.Fatal(err)
				}
				stdout = string(b)
			}
			te := clirun.ParseToolError([]byte(stdout), nil)
			if te == nil {
				t.Fatal("fixture holds no tool error")
			}
			// The adapters return the *ExitError wrapped, so wrap it too.
			failure := fmt.Errorf("source: %w", &clirun.ExitError{Call: clirun.Call{Bin: tc.tool}, Tool: te})
			path := serve(t, &Server{Handler: func(context.Context, string, json.RawMessage) (any, error) { return nil, failure }})
			c, err := Dial(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var e *Error
			err = c.Call(context.Background(), "any", nil, nil)
			if !errors.As(err, &e) || e.Code != tc.code || e.Message != tc.message {
				t.Fatalf("got %T %v, want code %q message %q", err, err, tc.code, tc.message)
			}
		})
	}
}

func TestOtherErrorsAreInternal(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var e *Error
	err = c.Call(context.Background(), "plain", nil, nil)
	if !errors.As(err, &e) || e.Code != "internal" || e.Message != "something broke" {
		t.Fatalf("got %T %v", err, err)
	}
}

func TestErrorOf(t *testing.T) {
	own := &Error{Code: "mine", Message: "m"}
	if got := ErrorOf(fmt.Errorf("wrapped: %w", own)); got.Code != "mine" || got.Message != "m" {
		t.Fatalf("a wrapped *Error changed: %+v", got)
	}
	if got := ErrorOf(errors.New("x")); got.Code != "internal" || got.Message != "x" {
		t.Fatalf("plain error: %+v", got)
	}
}

func TestSocketIs0600AndDirectoryIs0700(t *testing.T) {
	old := syscall.Umask(0) // a permissive umask must not widen the modes
	defer syscall.Umask(old)

	root := shortDir(t)
	dir := filepath.Join(root, "a", "b")
	path := filepath.Join(dir, "ork.sock")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	sock, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if sock.Mode()&os.ModeSocket == 0 || sock.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", sock.Mode())
	}
	for _, d := range []string{dir, filepath.Join(root, "a")} {
		info, err := os.Stat(d)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode %v", d, info.Mode().Perm())
		}
	}
}

func TestConnectionHooks(t *testing.T) {
	var opened, closed atomic.Int32
	path := serve(t, &Server{Handler: echo, OnConnect: func() { opened.Add(1) }, OnDisconnect: func() { closed.Add(1) }})
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Call(context.Background(), "echo", nil, nil); err != nil {
		t.Fatal(err)
	}
	if opened.Load() != 1 || closed.Load() != 0 {
		t.Fatalf("while connected: opened %d, closed %d", opened.Load(), closed.Load())
	}
	c.Close()
	waitFor(t, func() bool { return closed.Load() == 1 })
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBadJSONGetsAnErrorAndClose(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	reply, conn := rawExchange(t, path, "this is not json\n")
	var resp Response
	if err := json.Unmarshal([]byte(reply), &resp); err != nil || resp.Error == nil || resp.Error.Code != "bad_request" {
		t.Fatalf("reply %q: %+v, %v", reply, resp, err)
	}
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(conn).ReadString('\n'); err == nil {
		t.Fatal("connection stayed open after a bad message")
	}
}

func TestRequestWithoutMethodIsRefusedAndConnectionStays(t *testing.T) {
	path := serve(t, &Server{Handler: echo})
	reply, conn := rawExchange(t, path, `{"id":1}`+"\n")
	var resp Response
	if err := json.Unmarshal([]byte(reply), &resp); err != nil || resp.Error == nil || resp.Error.Code != "bad_request" || string(resp.ID) != "1" {
		t.Fatalf("reply %q: %+v, %v", reply, resp, err)
	}
	conn.Write([]byte(`{"id":2,"method":"echo","params":5}` + "\n"))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reply2, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(reply2, `"result":5`) {
		t.Fatalf("second reply %q: %v", reply2, err)
	}
}

func TestCancelStopsServeAndClosesClients(t *testing.T) {
	path := filepath.Join(shortDir(t), "d", "ork.sock")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	var closed atomic.Int32
	s := &Server{Handler: echo, OnDisconnect: func() { closed.Add(1) }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l) }()

	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(context.Background(), "echo", nil, nil); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if closed.Load() != 1 {
		t.Fatalf("OnDisconnect ran %d times by the time Serve returned", closed.Load())
	}
	if err := c.Call(context.Background(), "echo", nil, nil); err == nil {
		t.Fatal("a call on a closed server succeeded")
	}
	if _, err := net.Dial("unix", path); err == nil {
		t.Fatal("the listener still accepts")
	}
}

func TestCallHonoursItsContext(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	path := serve(t, &Server{Handler: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}})
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = c.Call(ctx, "slow", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %T %v", err, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("Call took %v", time.Since(start))
	}
	once.Do(func() { close(release) })
}

func TestDialFailsWhenNothingListens(t *testing.T) {
	if _, err := Dial(context.Background(), filepath.Join(shortDir(t), "none.sock")); err == nil {
		t.Fatal("Dial succeeded on a missing socket")
	}
}

func TestHandlerPanicBecomesInternalError(t *testing.T) {
	path := serve(t, &Server{Handler: func(context.Context, string, json.RawMessage) (any, error) { panic("boom") }})
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var e *Error
	err = c.Call(context.Background(), "x", nil, nil)
	if !errors.As(err, &e) || e.Code != "internal" || !strings.Contains(e.Message, "boom") {
		t.Fatalf("got %T %v", err, err)
	}
	if err := c.Call(context.Background(), "x", nil, nil); !errors.As(err, &e) {
		t.Fatalf("the server stopped answering after a panic: %v", err)
	}
}

func TestReplyWithAnotherIdIsAnError(t *testing.T) {
	path := filepath.Join(shortDir(t), "d", "ork.sock")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		bufio.NewReader(conn).ReadString('\n')
		conn.Write([]byte(`{"id":999,"result":1}` + "\n"))
	}()
	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = c.Call(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "999") {
		t.Fatalf("a reply for another request was accepted: %v", err)
	}
}

func TestExistingDirectoryIsMadePrivate(t *testing.T) {
	dir := filepath.Join(shortDir(t), "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // not narrowed by the umask
		t.Fatal(err)
	}
	l, err := Listen(filepath.Join(dir, "ork.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %v, %v", info.Mode().Perm(), err)
	}
}

func TestListenRefusesAPathPastTheSocketLimit(t *testing.T) {
	root := shortDir(t)
	// The longest path macOS accepts is 103 bytes (104 with the NUL).
	name := func(total int) string {
		dir := filepath.Join(root, "d")
		return dir + "/" + strings.Repeat("s", total-len(dir)-1)
	}
	if len(root) > 90 {
		t.Skip("temp directory too long for this test")
	}
	ok := name(103)
	l, err := Listen(ok)
	if err != nil {
		t.Fatalf("a 103 byte path must work: %v", err)
	}
	l.Close()

	tooLong := name(104)
	if _, err := Listen(tooLong); err == nil || !strings.Contains(err.Error(), "104") || !strings.Contains(err.Error(), tooLong) {
		t.Fatalf("a 104 byte path: %v", err)
	}
	deep := filepath.Join(root, "x", strings.Repeat("y", 110), "ork.sock")
	if _, err := Listen(deep); err == nil {
		t.Fatal("a long path was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
		t.Fatal("a directory was created for a path that is refused")
	}
}

// flakyListener fails its first Accept calls with a non-closed error.
type flakyListener struct {
	net.Listener
	fails atomic.Int32
}

func (f *flakyListener) Accept() (net.Conn, error) {
	if f.fails.Add(-1) >= 0 {
		return nil, errors.New("accept: too many open files")
	}
	return f.Listener.Accept()
}

func TestAcceptErrorDoesNotEndServe(t *testing.T) {
	path := filepath.Join(shortDir(t), "d", "ork.sock")
	inner, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	l := &flakyListener{Listener: inner}
	l.fails.Store(3)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&Server{Handler: echo}).Serve(ctx, l) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after cancel")
		}
	}()

	c, err := Dial(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	if err := c.Call(ctx2, "echo", nil, nil); err != nil {
		t.Fatalf("Serve gave up after an Accept error: %v", err)
	}
	select {
	case err := <-done:
		t.Fatalf("Serve returned: %v", err)
	default:
	}
}

func TestCancelEndsServeWhileAcceptKeepsFailing(t *testing.T) {
	path := filepath.Join(shortDir(t), "d", "ork.sock")
	inner, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	l := &flakyListener{Listener: inner}
	l.fails.Store(1 << 30)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&Server{Handler: echo}).Serve(ctx, l) }()
	time.Sleep(1500 * time.Millisecond) // the back-off has grown to its 1 s cap
	cancelled := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if took := time.Since(cancelled); took > 500*time.Millisecond {
		t.Fatalf("Serve took %v to notice the cancel; it waited out the back-off", took)
	}
}
