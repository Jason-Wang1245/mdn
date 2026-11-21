package copilot

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"mdn/internal/lsp"
	"mdn/internal/textpos"
)

const fakeEnv = "MDN_FAKE_COPILOT_SERVER"

// TestHelperProcess is not a real test: when fakeEnv is set it runs a fake
// copilot-language-server on stdin/stdout.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(fakeEnv) != "1" {
		t.Skip("helper process")
	}
	runFakeServer()
	os.Exit(0)
}

func runFakeServer() {
	var (
		mu   sync.Mutex
		doc  string
		conn *lsp.Conn
	)
	done := make(chan struct{})
	notify := func(method string, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct{ Text string } `json:"textDocument"`
			}
			_ = json.Unmarshal(params, &p)
			doc = p.TextDocument.Text
		case "textDocument/didChange":
			var p struct {
				ContentChanges []struct {
					Range Range  `json:"range"`
					Text  string `json:"text"`
				} `json:"contentChanges"`
			}
			_ = json.Unmarshal(params, &p)
			for _, ch := range p.ContentChanges {
				s := offsetOf(doc, ch.Range.Start)
				e := offsetOf(doc, ch.Range.End)
				doc = doc[:s] + ch.Text + doc[e:]
			}
		case "exit":
			close(done)
		}
	}
	request := func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "initialize":
			return map[string]any{"capabilities": map[string]any{}}, nil
		case "shutdown":
			return nil, nil
		case "signIn":
			return SignInResult{
				Status: "PromptUserDeviceFlow", UserCode: "ABCD-EFGH",
				VerificationURI: "https://github.com/login/device",
				Command:         &Command{Command: "github.copilot.finishDeviceFlow", Title: "Sign in"},
			}, nil
		case "workspace/executeCommand":
			var res struct {
				Success bool `json:"success"`
			}
			if err := conn.Call(ctx, "window/showDocument", map[string]any{"uri": "https://github.com/login/device", "external": true}, &res); err != nil || !res.Success {
				return nil, &lsp.Error{Code: lsp.CodeInternalError, Message: "showDocument failed"}
			}
			_ = conn.Notify("didChangeStatus", Status{Kind: StatusNormal, Message: ""})
			return nil, nil
		case "textDocument/inlineCompletion":
			var p struct {
				Position Position `json:"position"`
			}
			_ = json.Unmarshal(params, &p)
			mu.Lock()
			line := strings.Split(doc, "\n")[p.Position.Line]
			mu.Unlock()
			// Echo the current line back so the test can verify document sync.
			return map[string]any{"items": []map[string]any{{
				"insertText": line + "_done",
				"range":      Range{Start: Position{p.Position.Line, 0}, End: p.Position},
				"command":    Command{Command: "github.copilot.didAcceptCompletionItem"},
			}}}, nil
		}
		return nil, &lsp.Error{Code: lsp.CodeMethodNotFound, Message: method}
	}
	conn = lsp.NewConn(os.Stdin, os.Stdout, notify, request)
	select {
	case <-done:
	case <-conn.Done():
	}
}

func offsetOf(text string, p Position) int {
	off := 0
	for i := 0; i < p.Line; i++ {
		off += strings.IndexByte(text[off:], '\n') + 1
	}
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text) - off
	}
	line := text[off : off+end]
	col := textpos.UTF16ToRuneCol(line, p.Character)
	return off + len(string([]rune(line)[:col]))
}

func startFake(t *testing.T, opts Options) *Client {
	t.Helper()
	t.Setenv(fakeEnv, "1")
	opts.Command = []string{os.Args[0], "-test.run=^TestHelperProcess$"}
	opts.Logger = log.New(io.Discard, "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestClientSyncAndCompletion(t *testing.T) {
	c := startFake(t, Options{RootDir: t.TempDir()})
	uri := FileURI("/tmp/x.go")
	text := "package main\n\nfunc 😀() {\n"
	if err := c.DidOpen(uri, "go", 1, text); err != nil {
		t.Fatal(err)
	}
	edits := []string{
		"package main\n\nfunc 😀(n) {\n",
		"package main\n\nfunc 😀(n int) {\n\tret",
		"package main\n\nfunc 😀(n int) {\n\treturn",
	}
	v := 1
	for _, e := range edits {
		v++
		if err := c.DidChange(uri, v, text, e); err != nil {
			t.Fatal(err)
		}
		text = e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	items, err := c.InlineCompletion(ctx, uri, v, Position{Line: 3, Character: 7}, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].InsertText != "\treturn_done" {
		t.Fatalf("items = %+v", items)
	}
	if len(items[0].Raw) == 0 || items[0].Command == nil {
		t.Fatalf("raw/command not preserved: %+v", items[0])
	}
	if err := c.DidAccept(ctx, items[0]); err == nil {
		// Fake server doesn't know didAcceptCompletionItem; it must answer with
		// an error rather than hang.
		t.Log("accept command accepted")
	}
}

func TestClientSignInFlow(t *testing.T) {
	var (
		mu      sync.Mutex
		opened  string
		statusC = make(chan Status, 4)
	)
	c := startFake(t, Options{
		OnShowDocument: func(u string) { mu.Lock(); opened = u; mu.Unlock() },
		OnStatus:       func(s Status) { statusC <- s },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := c.SignIn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadySignedIn() || res.UserCode != "ABCD-EFGH" || res.Command == nil {
		t.Fatalf("signIn = %+v", res)
	}
	if err := c.FinishSignIn(ctx, res); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if opened != "https://github.com/login/device" {
		t.Fatalf("showDocument uri = %q", opened)
	}
	mu.Unlock()
	select {
	case s := <-statusC:
		if s.Kind != StatusNormal {
			t.Fatalf("status = %+v", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no status notification")
	}
}

func TestParseItemsArrayForm(t *testing.T) {
	items, err := parseItems(json.RawMessage(`[{"insertText":"a"},{"insertText":"b"}]`))
	if err != nil || len(items) != 2 || items[1].InsertText != "b" {
		t.Fatalf("%v %+v", err, items)
	}
}
