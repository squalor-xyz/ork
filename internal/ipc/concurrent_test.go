// SPDX-License-Identifier: GPL-3.0-or-later

package ipc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestCallWhileNextWaits(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := &Client{conn: a, dec: json.NewDecoder(a), enc: json.NewEncoder(a)}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := c.Next(ctx); done <- err }()
	time.Sleep(10 * time.Millisecond)
	go func() {
		var req Request
		json.NewDecoder(b).Decode(&req)
		json.NewEncoder(b).Encode(Response{ID: req.ID, Result: json.RawMessage(`"ok"`)})
		json.NewEncoder(b).Encode(Notification{Method: "changed"})
	}()
	var result string
	if err := c.Call(ctx, "subscribe", nil, &result); err != nil {
		t.Fatalf("Call blocked behind Next: %v", err)
	}
	if result != "ok" {
		t.Fatalf("reply %q", result)
	}
	if err := <-done; err != nil {
		t.Fatalf("Next: %v", err)
	}
}

func TestReplyOverflowFailsClient(t *testing.T) {
	for _, method := range []string{"Next", "Call"} {
		t.Run(method, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			c := &Client{conn: a, dec: json.NewDecoder(a), enc: json.NewEncoder(a)}
			defer c.Close()
			c.startReader()
			enc := json.NewEncoder(b)
			for i := 1; i <= 2; i++ {
				if err := enc.Encode(Response{ID: json.RawMessage(`1`)}); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-c.done:
			case <-time.After(time.Second):
				t.Fatal("reader did not stop on reply overflow")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			var err error
			if method == "Next" {
				_, err = c.Next(ctx)
			} else {
				err = c.Call(ctx, "snapshot", nil, nil)
			}
			if err == nil || !strings.Contains(err.Error(), "reply overflow") {
				t.Fatalf("%s after unsolicited replies: got %v, want reply overflow error", method, err)
			}
		})
	}
}
