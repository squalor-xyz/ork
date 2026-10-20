// SPDX-License-Identifier: GPL-3.0-or-later

package ipc

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

func TestAfterReplyPreservesStopResponse(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	a.SetDeadline(time.Now().Add(time.Second))
	done := make(chan struct{})
	s := Server{Handler: func(context.Context, string, json.RawMessage) (any, error) { return "stopped", nil }, AfterReply: func(method string) { b.Close(); close(done) }}
	go s.serveConn(context.Background(), b)
	json.NewEncoder(a).Encode(Request{ID: json.RawMessage(`1`), Method: "daemon.stop"})
	var res Response
	if err := json.NewDecoder(a).Decode(&res); err != nil {
		t.Fatalf("lost stop response: %v", err)
	}
	if string(res.Result) != `"stopped"` {
		t.Fatalf("stop response: %s", res.Result)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback did not run")
	}
}
