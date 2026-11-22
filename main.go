// Command mdn is a single-file Markdown editor for the terminal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"mdn/internal/copilot"
	"mdn/internal/doc"
	"mdn/internal/ui"
)

func main() {
	var (
		serverPath = flag.String("server", "", "path to copilot-language-server (binary or language-server.js); env "+copilot.EnvServer)
		logPath    = flag.String("log", filepath.Join(os.TempDir(), "mdn.log"), "Copilot log file")
		noCopilot  = flag.Bool("no-copilot", false, "disable GitHub Copilot suggestions")
		login      = flag.Bool("login", false, "sign in to GitHub Copilot and exit")
		logout     = flag.Bool("logout", false, "sign out of GitHub Copilot and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: mdn [flags] [file.md]
       mdn --login | --logout

With no file, mdn edits its global notes file (override with $%s).

Flags:
`, doc.EnvFile)
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	logFile, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fatal(err)
	}
	defer logFile.Close()
	logger := log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)

	start := func(ctx context.Context, opts copilot.Options) (*copilot.Client, error) {
		argv, err := copilot.ServerCommand(*serverPath)
		if err != nil {
			return nil, err
		}
		logger.Printf("starting %v", argv)
		opts.Command = argv
		return copilot.Start(ctx, opts)
	}

	if *login || *logout {
		if err := headless(start, logger, *login); err != nil {
			fatal(err)
		}
		return
	}

	var p *tea.Program
	opts := ui.Options{Logger: logger, Send: func(msg tea.Msg) { p.Send(msg) }}
	if !*noCopilot {
		opts.StartCopilot = start
	}
	path := flag.Arg(0)
	if path == "" {
		if path, err = doc.DefaultPath(); err != nil {
			fatal(err)
		}
	}
	m, err := ui.New(path, opts)
	if err != nil {
		fatal(err)
	}
	p = tea.NewProgram(m)
	final, runErr := p.Run()
	if fm, ok := final.(ui.Model); ok {
		fm.Close()
	} else {
		m.Close()
	}
	if runErr != nil {
		fatal(runErr)
	}
}

// headless runs the Copilot sign-in / sign-out flow without the TUI.
func headless(start func(context.Context, copilot.Options) (*copilot.Client, error), logger *log.Logger, signIn bool) error {
	cwd, _ := os.Getwd()
	normal := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := start(ctx, copilot.Options{
		RootDir:     cwd,
		Logger:      logger,
		OpenBrowser: true,
		OnStatus: func(s copilot.Status) {
			if s.Kind == copilot.StatusNormal {
				select {
				case normal <- struct{}{}:
				default:
				}
			}
		},
		OnShowDocument: func(u string) { fmt.Printf("Opening %s …\n", u) },
		OnMessage:      func(s string) { fmt.Println(s) },
	})
	if err != nil {
		return err
	}
	defer c.Close()

	if !signIn {
		if err := c.SignOut(ctx); err != nil {
			return err
		}
		fmt.Println("Signed out of GitHub Copilot.")
		return nil
	}

	res, err := c.SignIn(ctx)
	if err != nil {
		return err
	}
	if res.AlreadySignedIn() {
		fmt.Printf("Already signed in%s.\n", userSuffix(res.User))
		return nil
	}
	uri := res.VerificationURI
	if uri == "" {
		uri = "https://github.com/login/device"
	}
	fmt.Printf("\nYour one-time code: %s\nEnter it at %s\n\n", res.UserCode, uri)
	fmt.Println("Waiting for authorization (Ctrl+C to abort)…")

	flowCtx, flowCancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer flowCancel()
	if err := c.FinishSignIn(flowCtx, res); err != nil {
		return fmt.Errorf("device flow: %w", err)
	}
	select {
	case <-normal:
	case <-time.After(10 * time.Second):
	}
	if s := c.Status(); s.Kind == copilot.StatusError {
		return errors.New("sign-in did not complete: " + s.Message)
	}
	fmt.Println("Signed in to GitHub Copilot.")
	return nil
}

func userSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " as " + u
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mdn:", err)
	os.Exit(1)
}
