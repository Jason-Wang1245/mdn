package copilot

import (
	"context"
	"log"
	"os"
	"testing"
	"time"
)

// TestLive talks to the real copilot-language-server. It only runs with
// MDN_LIVE=1 and requires an already signed-in account (it never starts a
// device flow). Set COPILOT_LANGUAGE_SERVER to choose the server binary.
func TestLive(t *testing.T) {
	if os.Getenv("MDN_LIVE") != "1" {
		t.Skip("set MDN_LIVE=1 to run against the real Copilot server")
	}
	argv, err := ServerCommand("")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := Start(ctx, Options{
		Command:  argv,
		RootDir:  t.TempDir(),
		Logger:   log.New(testWriter{t}, "", 0),
		OnStatus: func(s Status) { t.Logf("status: %+v", s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	res, err := c.SignIn(ctx)
	if err != nil {
		t.Fatalf("signIn: %v", err)
	}
	t.Logf("signIn: %+v", res)
	if !res.AlreadySignedIn() {
		t.Skip("not signed in; run `mdn --login` first")
	}

	uri := FileURI(t.TempDir() + "/notes.md")
	text := "# Making tea\n\n1. Boil the water.\n2. "
	if err := c.DidOpen(uri, "markdown", 1, text); err != nil {
		t.Fatal(err)
	}
	items, err := c.InlineCompletion(ctx, uri, 1, Position{Line: 3, Character: 3}, 2, true)
	if err != nil {
		t.Fatalf("inlineCompletion: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("no completions returned")
	}
	for i, it := range items {
		t.Logf("item %d: range=%+v text=%q", i, it.Range, it.InsertText)
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
