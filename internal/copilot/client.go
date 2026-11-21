// Package copilot drives GitHub's official copilot-language-server over LSP.
package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"mdn/internal/lsp"
	"mdn/internal/textpos"
)

// Version is reported to the language server as the editor/plugin version.
const Version = "0.1.0"

// Options configures Start.
type Options struct {
	// Command is the server argv; if empty ServerCommand("") is used.
	Command []string
	// RootDir is reported as the workspace folder.
	RootDir string
	// Logger receives server stderr and log messages. Defaults to discard.
	Logger *log.Logger

	// OnStatus is called for every didChangeStatus notification.
	OnStatus func(Status)
	// OnMessage is called for user-facing window/showMessage(Request) text.
	OnMessage func(msg string)
	// OnShowDocument is called when the server asks to open a URL (e.g. the
	// device-flow verification page). If OpenBrowser is true the URL is also
	// opened with the platform opener.
	OnShowDocument func(uri string)
	OpenBrowser    bool
}

// Client is a running Copilot language server session.
type Client struct {
	opts Options
	cmd  *exec.Cmd
	conn *lsp.Conn
	log  *log.Logger

	settings map[string]any

	mu     sync.Mutex
	status Status
}

// Start launches the language server and performs the LSP initialize handshake.
func Start(ctx context.Context, opts Options) (*Client, error) {
	if opts.Logger == nil {
		opts.Logger = log.New(io.Discard, "", 0)
	}
	argv := opts.Command
	if len(argv) == 0 {
		var err error
		if argv, err = ServerCommand(""); err != nil {
			return nil, err
		}
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stderr = opts.Logger.Writer()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start copilot-language-server: %w", err)
	}

	c := &Client{opts: opts, cmd: cmd, log: opts.Logger, settings: defaultSettings()}
	c.conn = lsp.NewConn(stdout, stdin, c.handleNotification, c.handleRequest)
	go func() {
		<-c.conn.Done()
		_ = cmd.Wait()
	}()

	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func defaultSettings() map[string]any {
	s := map[string]any{
		"telemetry": map[string]any{"telemetryLevel": "off"},
	}
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(k); v != "" {
			s["http"] = map[string]any{"proxy": v, "proxyStrictSSL": true}
			break
		}
	}
	return s
}

// FileURI converts a filesystem path to a file:// URI.
func FileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"clientInfo": map[string]any{
			"name": "mdn", "version": Version,
		},
		"capabilities": map[string]any{
			"workspace": map[string]any{"workspaceFolders": true, "configuration": true},
			"window":    map[string]any{"showDocument": map[string]any{"support": true}},
			"textDocument": map[string]any{
				"inlineCompletion": map[string]any{"dynamicRegistration": false},
			},
		},
		"initializationOptions": map[string]any{
			"editorInfo":       map[string]any{"name": "mdn", "version": Version},
			"editorPluginInfo": map[string]any{"name": "mdn-copilot", "version": Version},
		},
	}
	if c.opts.RootDir != "" {
		uri := FileURI(c.opts.RootDir)
		params["rootUri"] = uri
		params["workspaceFolders"] = []map[string]any{{"uri": uri, "name": filepath.Base(c.opts.RootDir)}}
	}
	if err := c.conn.Call(ctx, "initialize", params, nil); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := c.conn.Notify("initialized", map[string]any{}); err != nil {
		return err
	}
	return c.conn.Notify("workspace/didChangeConfiguration", map[string]any{"settings": c.settings})
}

// Status returns the last status reported by the server.
func (c *Client) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// --- authentication -------------------------------------------------------

// SignIn starts the GitHub device flow (or reports that the user is already
// signed in).
func (c *Client) SignIn(ctx context.Context) (SignInResult, error) {
	var res SignInResult
	err := c.conn.Call(ctx, "signIn", map[string]any{}, &res)
	return res, err
}

// FinishSignIn executes the command returned by SignIn. The server opens the
// verification page (via window/showDocument) and polls GitHub until the user
// authorises the device; this call returns when that completes.
func (c *Client) FinishSignIn(ctx context.Context, res SignInResult) error {
	if res.Command == nil {
		return errors.New("signIn returned no command to finish the device flow")
	}
	return c.ExecuteCommand(ctx, res.Command)
}

// SignOut signs the user out of Copilot.
func (c *Client) SignOut(ctx context.Context) error {
	return c.conn.Call(ctx, "signOut", map[string]any{}, nil)
}

// ExecuteCommand runs a server command via workspace/executeCommand.
func (c *Client) ExecuteCommand(ctx context.Context, cmd *Command) error {
	args := cmd.Arguments
	if args == nil {
		args = []json.RawMessage{}
	}
	return c.conn.Call(ctx, "workspace/executeCommand", map[string]any{
		"command": cmd.Command, "arguments": args,
	}, nil)
}

// --- document sync ----------------------------------------------------------

// DidOpen notifies the server that a document was opened and focuses it.
func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	if err := c.conn.Notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": version, "text": text,
		},
	}); err != nil {
		return err
	}
	return c.conn.Notify("textDocument/didFocus", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// DidChange sends an incremental change describing the edit from oldText to
// newText.
func (c *Client) DidChange(uri string, version int, oldText, newText string) error {
	start, oldEnd, newEnd := textpos.Diff(oldText, newText)
	sl, sc := textpos.LineCol(oldText, start)
	el, ec := textpos.LineCol(oldText, oldEnd)
	return c.conn.Notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{
			"range": Range{Start: Position{sl, sc}, End: Position{el, ec}},
			"text":  newText[start:newEnd],
		}},
	})
}

// DidClose notifies the server that the document was closed.
func (c *Client) DidClose(uri string) error {
	return c.conn.Notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// --- completions ------------------------------------------------------------

// InlineCompletion requests ghost-text completions at pos. invoked should be
// true when the user explicitly asked for a suggestion.
func (c *Client) InlineCompletion(ctx context.Context, uri string, version int, pos Position, tabSize int, invoked bool) ([]InlineCompletionItem, error) {
	trigger := 2 // Automatic
	if invoked {
		trigger = 1 // Invoked
	}
	var raw json.RawMessage
	err := c.conn.Call(ctx, "textDocument/inlineCompletion", map[string]any{
		"textDocument":      map[string]any{"uri": uri, "version": version},
		"position":          pos,
		"context":           map[string]any{"triggerKind": trigger},
		"formattingOptions": map[string]any{"tabSize": tabSize, "insertSpaces": true},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return parseItems(raw)
}

func parseItems(raw json.RawMessage) ([]InlineCompletionItem, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var list []json.RawMessage
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
	} else {
		var wrapper struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return nil, err
		}
		list = wrapper.Items
	}
	items := make([]InlineCompletionItem, 0, len(list))
	for _, r := range list {
		var it InlineCompletionItem
		if err := json.Unmarshal(r, &it); err != nil {
			continue
		}
		it.Raw = r
		items = append(items, it)
	}
	return items, nil
}

// DidShow reports that a completion was displayed to the user.
func (c *Client) DidShow(item InlineCompletionItem) error {
	return c.conn.Notify("textDocument/didShowCompletion", map[string]any{"item": item.Raw})
}

// DidAccept reports that a completion was fully accepted.
func (c *Client) DidAccept(ctx context.Context, item InlineCompletionItem) error {
	if item.Command == nil {
		return nil
	}
	return c.ExecuteCommand(ctx, item.Command)
}

// DidPartiallyAccept reports that the first acceptedLength UTF-16 units of
// item.InsertText were accepted.
func (c *Client) DidPartiallyAccept(item InlineCompletionItem, acceptedLength int) error {
	return c.conn.Notify("textDocument/didPartiallyAcceptCompletion", map[string]any{
		"item": item.Raw, "acceptedLength": acceptedLength,
	})
}

// --- lifecycle ----------------------------------------------------------------

// Close shuts the server down, killing it if it does not exit promptly.
func (c *Client) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	_ = c.conn.Call(ctx, "shutdown", nil, nil)
	cancel()
	_ = c.conn.Notify("exit", nil)
	select {
	case <-c.conn.Done():
	case <-time.After(time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
}

// --- server → client ------------------------------------------------------------

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "didChangeStatus":
		var s Status
		if err := json.Unmarshal(params, &s); err == nil {
			c.mu.Lock()
			c.status = s
			c.mu.Unlock()
			if c.opts.OnStatus != nil {
				c.opts.OnStatus(s)
			}
		}
	case "window/logMessage":
		var m struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &m) == nil {
			c.log.Printf("[server] %s", m.Message)
		}
	case "window/showMessage":
		var m struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &m) == nil {
			c.log.Printf("[showMessage] %s", m.Message)
			if c.opts.OnMessage != nil {
				c.opts.OnMessage(m.Message)
			}
		}
	default:
		c.log.Printf("[notify] %s %s", method, truncate(params, 200))
	}
}

func (c *Client) handleRequest(_ context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "window/showDocument":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		c.log.Printf("[showDocument] %s", p.URI)
		if c.opts.OnShowDocument != nil {
			c.opts.OnShowDocument(p.URI)
		}
		ok := true
		if c.opts.OpenBrowser {
			ok = OpenURL(p.URI) == nil
		}
		return map[string]any{"success": ok}, nil

	case "window/showMessageRequest":
		var p ShowMessageRequest
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		c.log.Printf("[showMessageRequest] %s", p.Message)
		if c.opts.OnMessage != nil {
			c.opts.OnMessage(p.Message)
		}
		return nil, nil // no action chosen

	case "workspace/configuration":
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		out := make([]any, len(p.Items))
		for i, it := range p.Items {
			if it.Section == "" {
				out[i] = c.settings
			} else {
				out[i] = c.settings[it.Section]
			}
		}
		return out, nil

	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
		return nil, nil
	}
	c.log.Printf("[request] unhandled %s", method)
	return nil, &lsp.Error{Code: lsp.CodeMethodNotFound, Message: "unhandled method " + method}
}

// OpenURL opens u with the platform's default handler.
func OpenURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
