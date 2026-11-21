package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// pipePair returns two connected Conns.
func pipePair(t *testing.T, aNotify NotificationHandler, aReq RequestHandler, bNotify NotificationHandler, bReq RequestHandler) (*Conn, *Conn) {
	t.Helper()
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	a := NewConn(ar, aw, aNotify, aReq)
	b := NewConn(br, bw, bNotify, bReq)
	t.Cleanup(func() { aw.Close(); bw.Close() })
	return a, b
}

func TestCallResponse(t *testing.T) {
	_, b := pipePair(t, nil, func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method != "add" {
			return nil, &Error{Code: CodeMethodNotFound, Message: method}
		}
		var p [2]int
		_ = json.Unmarshal(params, &p)
		return p[0] + p[1], nil
	}, nil, nil)

	var sum int
	if err := b.Call(context.Background(), "add", []int{2, 3}, &sum); err != nil {
		t.Fatal(err)
	}
	if sum != 5 {
		t.Fatalf("sum = %d", sum)
	}

	err := b.Call(context.Background(), "nope", nil, nil)
	rpcErr, ok := err.(*Error)
	if !ok || rpcErr.Code != CodeMethodNotFound {
		t.Fatalf("want MethodNotFound, got %v", err)
	}
}

func TestNotify(t *testing.T) {
	got := make(chan string, 1)
	a, _ := pipePair(t, nil, nil, func(method string, params json.RawMessage) {
		got <- method + " " + string(params)
	}, nil)
	if err := a.Notify("hello", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != `hello {"x":1}` {
			t.Fatalf("got %q", s)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestCancelSendsCancelRequest(t *testing.T) {
	// Raw peer that never answers but records what it receives.
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	t.Cleanup(func() { aw.Close(); bw.Close() })
	c := NewConn(ar, aw, nil, nil)
	_ = bw

	methods := make(chan string, 4)
	go func() {
		r := bufio.NewReader(br)
		for {
			body, err := readFrame(r)
			if err != nil {
				return
			}
			var m message
			_ = json.Unmarshal(body, &m)
			methods <- m.Method
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- c.Call(ctx, "slow", nil, nil) }()
	if m := <-methods; m != "slow" {
		t.Fatalf("first message %q", m)
	}
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
	select {
	case m := <-methods:
		if m != "$/cancelRequest" {
			t.Fatalf("second message %q", m)
		}
	case <-time.After(time.Second):
		t.Fatal("no $/cancelRequest sent")
	}
}

func TestCloseFailsPending(t *testing.T) {
	ar, bw := io.Pipe()
	c := NewConn(ar, io.Discard, nil, nil)
	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "x", nil, nil) }()
	time.Sleep(20 * time.Millisecond)
	bw.Close()
	select {
	case err := <-errc:
		if err != ErrClosed {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending call not released")
	}
}
