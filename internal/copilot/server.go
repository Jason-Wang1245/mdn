package copilot

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InstallHint tells users how to get the language server.
const InstallHint = "install it with: npm i -g @github/copilot-language-server"

// EnvServer overrides the language server location.
const EnvServer = "COPILOT_LANGUAGE_SERVER"

// ServerCommand returns the argv used to launch the Copilot language server.
//
// Resolution order: override (flag), $COPILOT_LANGUAGE_SERVER,
// `copilot-language-server` on PATH, then `npx @github/copilot-language-server`.
// An override pointing at a .js file is run with node.
func ServerCommand(override string) ([]string, error) {
	if override == "" {
		override = os.Getenv(EnvServer)
	}
	if override != "" {
		if strings.HasSuffix(override, ".js") {
			node, err := exec.LookPath("node")
			if err != nil {
				return nil, errors.New("node not found on PATH (needed to run " + filepath.Base(override) + ")")
			}
			return []string{node, override, "--stdio"}, nil
		}
		return []string{override, "--stdio"}, nil
	}
	if p, err := exec.LookPath("copilot-language-server"); err == nil {
		return []string{p, "--stdio"}, nil
	}
	if p, err := exec.LookPath("npx"); err == nil {
		return []string{p, "--yes", "@github/copilot-language-server", "--stdio"}, nil
	}
	return nil, errors.New("copilot-language-server not found; " + InstallHint)
}
