package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kayushkin/tool-store/internal/childprocess"
	"github.com/kayushkin/tool-store/schema"
)

// Shell returns a tool that executes shell commands via bash.
func Shell() Impl {
	type input struct {
		Command  string   `json:"command"`
		Commands []string `json:"commands"`
		Workdir  string   `json:"workdir"`
	}
	return Impl{
		Name:        "shell_commands",
		Description: "Run one or more shell commands. Use commands[] array to run multiple in sequence (preferred). Each turn is expensive — maximize work per call.",
		InputSchema: schema.Props([]string{}, map[string]any{
			"command":  schema.Str("Single shell command to execute"),
			"commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Multiple commands to run sequentially (preferred)"},
			"workdir":  schema.Str("Working directory (optional, defaults to cwd)"),
		}),
		Run: func(ctx context.Context, raw string) (string, error) {
			in, err := schema.Parse[input](raw)
			if err != nil {
				return "", err
			}

			// Collect commands to run.
			cmds := in.Commands
			if in.Command != "" {
				if len(cmds) == 0 {
					cmds = []string{in.Command}
				} else {
					cmds = append([]string{in.Command}, cmds...)
				}
			}
			if len(cmds) == 0 {
				return "error: provide 'command' or 'commands'", nil
			}

			var results []string
			for i, c := range cmds {
				// A cancelled call must not keep starting the commands behind
				// the one it stopped. Say how many were skipped rather than
				// returning a run that looks complete.
				if err := ctx.Err(); err != nil {
					results = append(results, fmt.Sprintf("(stopped: %s — %d of %d commands not run)", err, len(cmds)-i, len(cmds)))
					break
				}
				cmd := childprocess.NewCommand(ctx, "bash", "-c", c)
				if in.Workdir != "" {
					cmd.Dir = in.Workdir
				}
				// Ensure common dev tools are on PATH (mise, go, node, etc.)
				cmd.Env = ensureDevToolsOnPath()
				out, err := cmd.CombinedOutput()
				result := string(out)
				if err != nil {
					result = fmt.Sprintf("%s\nexit: %s", result, err)
				}
				if result == "" {
					result = "(no output)"
				}
				result = truncateShellOutput(result)
				if len(cmds) > 1 {
					results = append(results, fmt.Sprintf("=== $ %s ===\n%s", c, result))
				} else {
					results = append(results, result)
				}
			}
			return strings.Join(results, "\n\n"), nil
		},
	}
}

// truncateShellOutput applies intelligent truncation to shell output
func truncateShellOutput(s string) string {
	const (
		maxLines     = 500
		maxChars     = 50000
		headLines    = 250
		tailLines    = 200
		headChars    = 25000
		tailChars    = 20000
	)

	// Check character limit first
	if len(s) > maxChars {
		if len(s) <= headChars+tailChars {
			return s
		}
		head := s[:headChars]
		tail := s[len(s)-tailChars:]
		omitted := len(s) - headChars - tailChars
		return fmt.Sprintf("%s\n\n[... %d characters omitted ...]\n\n%s", head, omitted, tail)
	}

	// Check line limit
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}

	head := strings.Join(lines[:headLines], "\n")
	tail := strings.Join(lines[len(lines)-tailLines:], "\n")
	omitted := len(lines) - headLines - tailLines
	return fmt.Sprintf("%s\n\n[... %d lines omitted ...]\n\n%s", head, omitted, tail)
}

// ensureDevToolsOnPath returns os.Environ() with mise shim paths prepended to PATH.
func ensureDevToolsOnPath() []string {
	env := os.Environ()
	home, _ := os.UserHomeDir()
	if home == "" {
		return env
	}

	// Paths where mise installs tool shims/binaries
	extraPaths := []string{
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, "go", "bin"),
	}

	for i, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			existing := e[5:]
			env[i] = "PATH=" + strings.Join(extraPaths, ":") + ":" + existing
			return env
		}
	}
	return env
}
