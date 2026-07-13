package toolstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubstituteBasic(t *testing.T) {
	out, err := substitute("hello {{name}}", map[string]any{"name": "world"})
	if err != nil || out != "hello world" {
		t.Fatalf("got %q err=%v", out, err)
	}
}

func TestSubstituteMultiple(t *testing.T) {
	out, err := substitute("{{a}}-{{b}}-{{a}}", map[string]any{"a": "X", "b": "Y"})
	if err != nil || out != "X-Y-X" {
		t.Fatalf("got %q err=%v", out, err)
	}
}

func TestSubstituteSpaces(t *testing.T) {
	out, err := substitute("{{ key }}", map[string]any{"key": "v"})
	if err != nil || out != "v" {
		t.Fatalf("got %q err=%v", out, err)
	}
}

func TestSubstituteMissing(t *testing.T) {
	_, err := substitute("{{missing}}", map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestSubstituteNonString(t *testing.T) {
	out, err := substitute("count={{n}}", map[string]any{"n": float64(42)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if out != "count=42" {
		t.Fatalf("got %q", out)
	}
}

func TestSubstitutePassthrough(t *testing.T) {
	out, err := substitute("no placeholders", nil)
	if err != nil || out != "no placeholders" {
		t.Fatalf("got %q err=%v", out, err)
	}
}

func TestRunCLIEcho(t *testing.T) {
	tool := &Tool{
		Name: "echo",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:      "echo",
			ArgsTemplate: []string{"{{msg}}"},
		},
	}
	out, err := runCLI(context.Background(), tool, `{"msg":"hi"}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("output: %q", out)
	}
}

func TestRunCLIMissingField(t *testing.T) {
	tool := &Tool{
		Name: "echo",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:      "echo",
			ArgsTemplate: []string{"{{nope}}"},
		},
	}
	_, err := runCLI(context.Background(), tool, `{}`)
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestRunCLINonZeroExit(t *testing.T) {
	tool := &Tool{
		Name: "false",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command: "false",
		},
	}
	_, err := runCLI(context.Background(), tool, `{}`)
	if err == nil {
		t.Fatal("expected error for nonzero exit")
	}
}

// TestRunCLIMissingSpec covers the guard for a tool whose CLI spec is nil or
// has an empty command — runCLI must reject it rather than exec nothing.
func TestRunCLIMissingSpec(t *testing.T) {
	if _, err := runCLI(context.Background(), &Tool{Name: "nospec", Kind: KindCLI}, `{}`); err == nil {
		t.Fatal("expected error when CLI spec is nil")
	}
	if _, err := runCLI(context.Background(), &Tool{Name: "nocmd", Kind: KindCLI, CLI: &CLISpec{}}, `{}`); err == nil {
		t.Fatal("expected error when CLI command is empty")
	}
}

// TestRunCLIInvalidInputJSON covers rejection of malformed input JSON before
// any command runs.
func TestRunCLIInvalidInputJSON(t *testing.T) {
	tool := &Tool{Name: "echo", Kind: KindCLI, CLI: &CLISpec{Command: "echo"}}
	if _, err := runCLI(context.Background(), tool, `{not json`); err == nil {
		t.Fatal("expected error for invalid input json")
	}
}

// TestRunCLITimeout exercises the per-tool timeout: a sleep that outlasts
// TimeoutMs must be cancelled and surface an error rather than block.
func TestRunCLITimeout(t *testing.T) {
	tool := &Tool{
		Name: "slow",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:      "sleep",
			ArgsTemplate: []string{"5"},
			TimeoutMs:    50,
		},
	}
	start := time.Now()
	_, err := runCLI(context.Background(), tool, `{}`)
	if err == nil {
		t.Fatal("expected error from timed-out command")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("timeout did not fire promptly: ran for %v", elapsed)
	}
}

// TestRunCLIContextCancel covers cancellation via the caller's context (as
// opposed to the per-tool timeout) — a cancelled context must abort the run.
func TestRunCLIContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before running
	tool := &Tool{
		Name: "slow",
		Kind: KindCLI,
		CLI:  &CLISpec{Command: "sleep", ArgsTemplate: []string{"5"}},
	}
	if _, err := runCLI(ctx, tool, `{}`); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestRunCLIStderrCapture verifies that on a non-zero exit the stderr text is
// folded into the returned error while stdout is still returned to the caller.
func TestRunCLIStderrCapture(t *testing.T) {
	tool := &Tool{
		Name: "noisy",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:      "sh",
			ArgsTemplate: []string{"-c", "echo out; echo boom 1>&2; exit 3"},
		},
	}
	out, err := runCLI(context.Background(), tool, `{}`)
	if err == nil {
		t.Fatal("expected error for nonzero exit")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("stderr not folded into error: %v", err)
	}
	if strings.TrimSpace(out) != "out" {
		t.Fatalf("stdout not captured on failure: %q", out)
	}
}

// TestRunCLIEnvInjection verifies that env-var names listed in Tool.EnvKeys are
// passed through to the child process (and only those that are actually set).
func TestRunCLIEnvInjection(t *testing.T) {
	const key, val = "TOOLSTORE_TEST_ENV", "secret-value"
	t.Setenv(key, val)
	tool := &Tool{
		Name:    "printenv",
		Kind:    KindCLI,
		EnvKeys: []string{key},
		CLI: &CLISpec{
			Command:      "sh",
			ArgsTemplate: []string{"-c", "printf %s \"$" + key + "\""},
		},
	}
	out, err := runCLI(context.Background(), tool, `{}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != val {
		t.Fatalf("env var not injected: got %q want %q", out, val)
	}
}

// TestRunCLIWorkingDir verifies that CLISpec.WorkingDir sets the child's cwd.
func TestRunCLIWorkingDir(t *testing.T) {
	dir := t.TempDir()
	tool := &Tool{
		Name: "pwd",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:    "pwd",
			WorkingDir: dir,
		},
	}
	out, err := runCLI(context.Background(), tool, `{}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Resolve symlinks on both sides (e.g. macOS /tmp -> /private/tmp) before
	// comparing the reported cwd to the requested working dir.
	want, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(strings.TrimSpace(out))
	if got != want {
		t.Fatalf("working dir not honored: got %q want %q", got, want)
	}
}

// TestRunCLIArgsNonStringValue confirms non-string input values are stringified
// into a single argv element (no shell splitting) when passed through a
// placeholder.
func TestRunCLIArgsNonStringValue(t *testing.T) {
	tool := &Tool{
		Name: "echo",
		Kind: KindCLI,
		CLI: &CLISpec{
			Command:      "echo",
			ArgsTemplate: []string{"{{n}}"},
		},
	}
	out, err := runCLI(context.Background(), tool, `{"n": 42}`)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "42" {
		t.Fatalf("output: %q", out)
	}
}
