package toolstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openProvTest(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "ts"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProvisionStdioWithCreds(t *testing.T) {
	s := openProvTest(t)
	if _, err := s.UpsertTool(&Tool{
		Name:        "brave-search",
		Kind:        KindMCP,
		EnvKeys:     []string{"BRAVE_API_KEY"},
		Credentials: map[string]string{"BRAVE_API_KEY": "brave"},
		MCP: &MCPSpec{
			Transport: "stdio",
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-brave-search"},
		},
		Enabled: true,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	resolve := func(_ context.Context, provider string) (string, error) {
		if provider != "brave" {
			t.Fatalf("unexpected provider %q", provider)
		}
		return "secret-key", nil
	}
	resp, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"brave-search"}}, resolve)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	cfg, ok := resp.MCPServers["brave-search"]
	if !ok {
		t.Fatal("missing brave-search in response")
	}
	if cfg.Command != "npx" || len(cfg.Args) != 2 {
		t.Fatalf("config mismatch: %+v", cfg)
	}
	if cfg.Env["BRAVE_API_KEY"] != "secret-key" {
		t.Fatalf("env mismatch: %v", cfg.Env)
	}
}

func TestProvisionMissingMapping(t *testing.T) {
	s := openProvTest(t)
	// EnvKeys present but Credentials map missing the key
	_, _ = s.UpsertTool(&Tool{
		Name:    "brave",
		Kind:    KindMCP,
		EnvKeys: []string{"BRAVE_API_KEY"},
		MCP:     &MCPSpec{Transport: "stdio", Command: "npx"},
		Enabled: true,
	})
	_, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"brave"}}, func(context.Context, string) (string, error) {
		return "x", nil
	})
	if err == nil {
		t.Fatal("expected error for missing credentials mapping")
	}
}

func TestProvisionResolverError(t *testing.T) {
	s := openProvTest(t)
	_, _ = s.UpsertTool(&Tool{
		Name:        "brave",
		Kind:        KindMCP,
		EnvKeys:     []string{"BRAVE_API_KEY"},
		Credentials: map[string]string{"BRAVE_API_KEY": "brave"},
		MCP:         &MCPSpec{Transport: "stdio", Command: "npx"},
		Enabled:     true,
	})
	_, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"brave"}}, func(context.Context, string) (string, error) {
		return "", errors.New("not found")
	})
	if err == nil {
		t.Fatal("expected error when resolver fails")
	}
}

func TestProvisionDisabled(t *testing.T) {
	s := openProvTest(t)
	_, _ = s.UpsertTool(&Tool{
		Name:    "x",
		Kind:    KindMCP,
		MCP:     &MCPSpec{Transport: "stdio", Command: "npx"},
		Enabled: false,
	})
	_, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"x"}}, nil)
	if err == nil {
		t.Fatal("expected error for disabled tool")
	}
}

func TestProvisionRejectsNonMCP(t *testing.T) {
	s := openProvTest(t)
	_, _ = s.UpsertTool(&Tool{
		Name:    "shell",
		Kind:    KindLocal,
		Local:   &LocalSpec{Symbol: "shell"},
		Enabled: true,
	})
	_, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"shell"}}, nil)
	if err == nil {
		t.Fatal("expected error for non-mcp tool")
	}
}

func TestProvisionHTTP(t *testing.T) {
	s := openProvTest(t)
	_, _ = s.UpsertTool(&Tool{
		Name:    "remote-mcp",
		Kind:    KindMCP,
		MCP:     &MCPSpec{Transport: "http", URL: "https://example.com/mcp"},
		Enabled: true,
	})
	resp, err := Provision(context.Background(), s, ProvisionRequest{Tools: []string{"remote-mcp"}}, nil)
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	cfg := resp.MCPServers["remote-mcp"]
	if cfg.URL != "https://example.com/mcp" || cfg.Type != "http" {
		t.Fatalf("config: %+v", cfg)
	}
	if cfg.Command != "" {
		t.Fatal("stdio command should be empty for http transport")
	}
}

func TestProvisionEmptyToolsRejected(t *testing.T) {
	s := openProvTest(t)
	_, err := Provision(context.Background(), s, ProvisionRequest{}, nil)
	if err == nil {
		t.Fatal("expected error for empty tools list")
	}
}
