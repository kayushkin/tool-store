package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	
	"github.com/kayushkin/tool-store/schema"
)

// Grep returns a tool for searching file contents with grep/ripgrep.
// Models tend to batch search queries into a single call when a dedicated tool exists.
func Grep() Impl {
	type input struct {
		Pattern   string   `json:"pattern"`
		Paths     []string `json:"paths"`
		Recursive bool     `json:"recursive"`
		Flags     string   `json:"flags"`
	}
	return Impl{
		Name: "ripgrep",
		Description: `Search file contents for a pattern using ripgrep (rg). Returns matching lines with file:line prefix.
- pattern: regex pattern to search for
- paths: files or directories to search (default: current directory)
- recursive: search directories recursively (default: true)
- flags: extra flags like "-i" (case insensitive), "-l" (files only), "-w" (word match), "--type go"
Prefer this over shell grep — it respects .gitignore and is faster.`,
		InputSchema: schema.Props([]string{"pattern"}, map[string]any{
			"pattern":   schema.Str("Regex pattern to search for"),
			"paths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Files or directories to search (default: .)"},
			"recursive": schema.Bool("Search recursively (default: true)"),
			"flags":     schema.Str("Extra rg flags like -i, -l, -w, --type go"),
		}),
		Run: func(ctx context.Context, raw string) (string, error) {
			in, err := schema.Parse[input](raw)
			if err != nil {
				return "", err
			}

			// Build rg command. Fall back to grep -rn if rg isn't available.
			args := []string{"-n", "--no-heading"}
			if in.Flags != "" {
				args = append(args, strings.Fields(in.Flags)...)
			}
			args = append(args, in.Pattern)
			if len(in.Paths) > 0 {
				args = append(args, in.Paths...)
			} else {
				args = append(args, ".")
			}

			bin := "rg"
			if _, err := exec.LookPath("rg"); err != nil {
				// Fallback to grep
				bin = "grep"
				args = []string{"-rn"}
				if in.Flags != "" {
					args = append(args, strings.Fields(in.Flags)...)
				}
				args = append(args, in.Pattern)
				if len(in.Paths) > 0 {
					args = append(args, in.Paths...)
				}
			}

			cmd := exec.CommandContext(ctx, bin, args...)
			cmd.Env = ensureDevToolsOnPath()
			out, err := cmd.CombinedOutput()
			result := strings.TrimSpace(string(out))

			if result == "" {
				return "no matches found", nil
			}

			// Truncate if too many results
			lines := strings.Split(result, "\n")
			if len(lines) > 200 {
				result = strings.Join(lines[:200], "\n") + fmt.Sprintf("\n... (%d more matches)", len(lines)-200)
			}

			return result, nil
		},
	}
}
