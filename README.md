# mdn

A single-file Markdown editor for the terminal, with a live rendered preview,
a `/` command menu for inserting blocks, and optional GitHub Copilot
suggestions as ghost text.

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea),
[Lip Gloss](https://github.com/charmbracelet/lipgloss) and
[Glamour](https://github.com/charmbracelet/glamour).

## Features

- **Three views**: edit, read (rendered Markdown), and side-by-side split.
- **Slash commands**: type `/` to insert headings, lists, to-dos, quotes,
  callouts, code blocks, tables, links, images, math blocks, collapsible
  sections, dividers, and date/time stamps.
- **Copilot ghost text**: inline suggestions from the GitHub Copilot language
  server, accepted whole or word by word.
- **Safe saves**: files are written atomically (temp file + rename) and keep
  their existing permissions. You are asked before quitting with unsaved changes.
- **Light/dark aware**: the preview follows your terminal's background color.

## Requirements

- Go **1.27.3** or newer (see `go.mod`)
- Optional, for Copilot: Node.js plus the Copilot language server
  ```sh
  npm i -g @github/copilot-language-server
  ```
  If it isn't installed globally, mdn falls back to `npx @github/copilot-language-server`.

mdn runs on macOS, Linux and Windows. It is pure Go with no cgo.

## Install

```sh
git clone <repo-url> mdn
cd mdn
go build -o mdn .
```

To cross-compile, for example for Linux from a Mac:

```sh
GOOS=linux GOARCH=amd64 go build -o mdn-linux .   # or GOARCH=arm64
```

## Usage

```sh
mdn notes.md        # open (or create on first save) notes.md
mdn                 # start an untitled buffer; you'll be asked for a name on save
mdn --no-copilot    # run without Copilot
```

### Flags

| Flag | Description |
| --- | --- |
| `--server PATH` | Path to `copilot-language-server` (a binary or `language-server.js`). Overrides `$COPILOT_LANGUAGE_SERVER`. |
| `--log PATH` | Copilot log file (default: `$TMPDIR/mdn.log`). |
| `--no-copilot` | Disable GitHub Copilot suggestions. |
| `--login` | Sign in to GitHub Copilot and exit. |
| `--logout` | Sign out of GitHub Copilot and exit. |

## Keys

Shortcuts are listed as `cmd+…`. Terminals that support the kitty keyboard
protocol (Ghostty, kitty, WezTerm) pass Cmd through. In terminals that keep
Cmd for themselves (Terminal.app, or iTerm2 by default), and on Linux and
Windows, use `ctrl+…` instead. The exceptions are `cmd+e` and `cmd+a`, whose
Ctrl forms are `ctrl+r` and `ctrl+o`: `ctrl+a` and `ctrl+e` move to the line
start / end, because many terminals send them for `home` / `end`.

### General

| Key | Action |
| --- | --- |
| `cmd+s` | Save |
| `cmd+q` | Quit |
| `cmd+g` / `F1` | Toggle help |
| `cmd+e` / `ctrl+r` | Toggle edit / read view |
| `cmd+\` | Toggle split view |
| `pgup` / `pgdn` | Scroll |

### Editing

| Key | Action |
| --- | --- |
| Click / drag | Place the cursor / select text (drag past the top or bottom edge to scroll) |
| `cmd+c` / `cmd+x` / `cmd+v` | Copy / cut / paste |
| `cmd+a` / `ctrl+o` | Select all |
| `tab` | Indent (or accept a Copilot suggestion) |
| `home` / `end`, `ctrl+a` / `ctrl+e`, `cmd+←` / `cmd+→` | Line start / end |
| `ctrl+home` / `ctrl+end`, `cmd+↑` / `cmd+↓` | Document start / end |

### Copilot

| Key | Action |
| --- | --- |
| `tab` | Accept suggestion |
| `ctrl+→` / `alt+→` | Accept next word |
| `esc` | Dismiss suggestion |
| `alt+]` / `alt+[` | Next / previous suggestion |
| `ctrl+space` | Request a suggestion now |
| `cmd+l` | Sign in to Copilot |

### Slash commands

Type `/` at the start of a word to open the menu. Keep typing to filter it,
use `↑`/`↓` to pick a command, `enter` to apply it, and `esc` to close the menu.

| Command | Inserts |
| --- | --- |
| `/h1` … `/h6` | Heading (replaces the current line's prefix) |
| `/text` | Plain paragraph (removes the prefix) |
| `/bullet`, `/numbered`, `/todo` | `- `, `1. `, `- [ ] ` |
| `/quote`, `/callout` | `> `, `> [!note] ` |
| `/code` | Fenced code block |
| `/table` | 3-column table |
| `/divider` | `---` |
| `/link`, `/image` | `[](url)`, `![](url)` |
| `/math` | `$$ … $$` block |
| `/details` | `<details>` collapsible section |
| `/date`, `/time`, `/now` | Current date, time, or both |

Most commands also match aliases, such as `/ul`, `/task`, `/hr` and `/today`.

## GitHub Copilot

Suggestions need a GitHub account with Copilot access. To sign in, either
press `cmd+l` inside the editor or run:

```sh
mdn --login
```

mdn shows a one-time code and opens `https://github.com/login/device` in your
browser. The code is also copied to the clipboard, if your terminal supports
OSC 52. On a headless machine or over SSH, open the URL yourself and enter the
code.

The language server is found in this order:

1. `--server`
2. `$COPILOT_LANGUAGE_SERVER`
3. `copilot-language-server` on `PATH`
4. `npx --yes @github/copilot-language-server`

A path ending in `.js` is run with `node`. Diagnostics are written to the log
file (see `--log`).

## Development

```sh
go test ./...
```

The live Copilot test talks to the real language server. It needs an account
that is already signed in:

```sh
MDN_LIVE=1 go test ./internal/copilot -run TestLive
```

### Layout

```
main.go              CLI flags, headless login/logout, program startup
internal/ui          Bubble Tea model: editor, preview, slash menu, ghost text
internal/copilot     Copilot language-server client and server discovery
internal/lsp         JSON-RPC / LSP connection over stdio
internal/doc         Loading and atomic saving of the document
internal/textpos     Rune column / byte offset / LSP UTF-16 position conversions
```
