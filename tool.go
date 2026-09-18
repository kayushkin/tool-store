package toolstore

import "encoding/json"

type Kind string

const (
	KindMCP   Kind = "mcp"
	KindCLI   Kind = "cli"
	KindLocal Kind = "local"
)

func (k Kind) Valid() bool {
	switch k {
	case KindMCP, KindCLI, KindLocal:
		return true
	}
	return false
}

// Tool is a registry entry. Exactly one of MCP/CLI/Local is populated based on
// Kind. EnvKeys lists required environment variable *names* — values are
// resolved at provision time from the canonical credential source, never
// stored here. Credentials (optional) maps each env-var name to the
// auth-store provider that owns its value; entries with no mapping must be
// resolved by the caller out of band (and provisioning fails if neither path
// produces a value — no silent fallback).
type Tool struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name,omitempty"`
	Description string            `json:"description"`
	Kind        Kind              `json:"kind"`
	InputSchema json.RawMessage   `json:"input_schema,omitempty"`
	EnvKeys     []string          `json:"env_keys,omitempty"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Tags        []string          `json:"tags,omitempty"`

	MCP   *MCPSpec   `json:"mcp,omitempty"`
	CLI   *CLISpec   `json:"cli,omitempty"`
	Local *LocalSpec `json:"local,omitempty"`

	Enabled   bool  `json:"enabled"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

// MCPSpec describes how to launch or reach an MCP server.
type MCPSpec struct {
	Transport string   `json:"transport"`         // stdio | http | sse
	Command   string   `json:"command,omitempty"` // stdio
	Args      []string `json:"args,omitempty"`    // stdio
	URL       string   `json:"url,omitempty"`     // http | sse
}

// CLISpec describes an arbitrary command-line tool. ArgsTemplate elements may
// contain {{var}} placeholders that are substituted from the JSON-decoded input
// at invocation time.
type CLISpec struct {
	Command      string   `json:"command"`
	ArgsTemplate []string `json:"args_template,omitempty"`
	WorkingDir   string   `json:"working_dir,omitempty"`
	TimeoutMs    int      `json:"timeout_ms,omitempty"`
}

// LocalSpec describes a Go function registered into the tool-store binary at
// build time. Symbol is the canonical name (e.g. "agentkit/tools.Shell") used
// to look up the implementation in the in-process registry.
type LocalSpec struct {
	Symbol string `json:"symbol"`
}

// LocalDescriptor describes one in-process registered local tool — what's
// available to be enabled via POST /tools.
type LocalDescriptor struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}
