// Command mdn is a single-file Markdown editor for the terminal.
package main

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"

	"mdn/internal/ui"
)

func main() {
	if len(os.Args) > 2 || (len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help")) {
		fmt.Fprintln(os.Stderr, "usage: mdn [file.md]")
		os.Exit(2)
	}
	path := ""
	if len(os.Args) == 2 {
		path = os.Args[1]
	}

	m, err := ui.New(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdn:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "mdn:", err)
		os.Exit(1)
	}
}
