// Package lsp implements a minimal JSON-RPC 2.0 connection with LSP
// Content-Length framing, sufficient to drive a language server over stdio.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message) }

// Standard error codes.
const (
	CodeMethodNotFound   = -32601
	CodeInternalError    = -32603
	CodeRequestCancelled = -32800
)

// ErrClosed is returned for calls on a closed connection.
var ErrClosed = errors.New("lsp: connection closed")

// message is the wire envelope for requests, notifications and responses.
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// NotificationHandler handles a server→client notification.
type NotificationHandler func(method string, params json.RawMessage)

// RequestHandler handles a server→client request; it returns the result or an
// error (use *Error for a specific JSON-RPC code).
type RequestHandler func(ctx context.Context, method string, params json.RawMessage) (any, error)

type response struct {
	result json.RawMessage
	err    error
}

// Conn is a bidirectional JSON-RPC connection.
type Conn struct {
	w   io.Writer
	wmu sync.Mutex

	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan response
	closed  bool
	done    chan struct{}
	readErr error

	onNotify  NotificationHandler
	onRequest RequestHandler
}

// NewConn creates a connection reading from r and writing to w. Handlers may be
// nil. Call Run (usually in a goroutine) to start processing incoming messages.
func NewConn(r io.Reader, w io.Writer, onNotify NotificationHandler, onRequest RequestHandler) *Conn {
	c := &Conn{
		w:         w,
		pending:   make(map[int64]chan response),
		done:      make(chan struct{}),
		onNotify:  onNotify,
		onRequest: onRequest,
	}
	go c.readLoop(bufio.NewReader(r))
	return c
}

// Done is closed when the read loop exits.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns the error that terminated the read loop, if any.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readErr
}

// Call sends a request and waits for its response, decoding the result into
// result (which may be nil). If ctx is cancelled first, a $/cancelRequest
// notification is sent and ctx.Err() is returned.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	ch := make(chan response, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.pending[id] = ch
	c.mu.Unlock()

	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.send(message{ID: &rawID, Method: method, Params: mustMarshal(params)}); err != nil {
		c.forget(id)
		return err
	}

	select {
	case resp := <-ch:
		if resp.err != nil {
			return resp.err
		}
		if result != nil && len(resp.result) > 0 && string(resp.result) != "null" {
			return json.Unmarshal(resp.result, result)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	}
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	return c.send(message{Method: method, Params: mustMarshal(params)})
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Conn) send(m message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func mustMarshal(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("lsp: marshal params: %v", err))
	}
	return b
}

func (c *Conn) readLoop(r *bufio.Reader) {
	var err error
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.readErr = err
		for id, ch := range c.pending {
			ch <- response{err: ErrClosed}
			delete(c.pending, id)
		}
		c.mu.Unlock()
		close(c.done)
	}()

	for {
		var body []byte
		body, err = readFrame(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return
		}
		var m message
		if jerr := json.Unmarshal(body, &m); jerr != nil {
			continue // ignore malformed frames
		}
		c.dispatch(m)
	}
}

func (c *Conn) dispatch(m message) {
	switch {
	case m.Method != "" && m.ID != nil: // request from server
		go c.handleRequest(m)
	case m.Method != "": // notification
		if c.onNotify != nil {
			c.onNotify(m.Method, m.Params)
		}
	case m.ID != nil: // response
		id, err := strconv.ParseInt(strings.Trim(string(*m.ID), `"`), 10, 64)
		if err != nil {
			return
		}
		c.mu.Lock()
		ch, ok := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if !ok {
			return
		}
		if m.Error != nil {
			ch <- response{err: m.Error}
		} else {
			ch <- response{result: m.Result}
		}
	}
}

func (c *Conn) handleRequest(m message) {
	var (
		result any
		err    error
	)
	if c.onRequest == nil {
		err = &Error{Code: CodeMethodNotFound, Message: "method not found: " + m.Method}
	} else {
		result, err = c.onRequest(context.Background(), m.Method, m.Params)
	}
	reply := message{ID: m.ID}
	if err != nil {
		var rpcErr *Error
		if !errors.As(err, &rpcErr) {
			rpcErr = &Error{Code: CodeInternalError, Message: err.Error()}
		}
		reply.Error = rpcErr
	} else {
		b, merr := json.Marshal(result)
		if merr != nil {
			reply.Error = &Error{Code: CodeInternalError, Message: merr.Error()}
		} else {
			reply.Result = b
		}
	}
	_ = c.send(reply)
}

// readFrame reads one Content-Length framed message body.
func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length < 0 {
				continue // tolerate stray blank lines before headers
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", value)
			}
			length = n
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
