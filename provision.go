package toolstore

import (
	"context"
	"errors"
	"fmt"
)

// ProvisionRequest says which tools to provision, in one of two ways.
//
// Either name them outright in Tools, or name an instance in InstanceID and
// let the store read that instance's opt-in list (the rows the Tools page
// writes through /instances/{id}/tools/by-name/{name}). Exactly one of the
// two must be set: an empty request is an error — the caller must say what it
// wants, no implicit "all" — and a request carrying both is an error too,
// because merging a standing preference with a per-call list gives the same
// field two sources of truth.
type ProvisionRequest struct {
	Tools      []string `json:"tools"`
	InstanceID string   `json:"instance_id"`
}

// ProvisionResponse mirrors the shape Claude Code expects in --mcp-config:
//
//	{ "mcpServers": { "<name>": { "command": ..., "args": ..., "env": {...} } } }
//
// For non-stdio MCP servers, "command" is omitted and "url" + "type" describe
// the http/sse endpoint instead.
type ProvisionResponse struct {
	MCPServers map[string]MCPServerConfig `json:"mcpServers,omitempty"`
}

// MCPServerConfig is a single entry under "mcpServers". Field choice depends
// on the source MCPSpec.Transport.
type MCPServerConfig struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"`
}

// ResolveCredentialFunc resolves a credential for an auth-store provider name
// to its active key/token. Returns an error if the provider is unknown or no
// credential is enabled — provisioning fails loudly rather than producing a
// half-configured tool.
type ResolveCredentialFunc func(ctx context.Context, provider string) (string, error)

// Provision builds the MCP server config for the requested tool names. Errors
// out (no fallback) if any tool is missing, disabled, or has env-key
// resolution gaps.
func Provision(ctx context.Context, s *Store, req ProvisionRequest, resolve ResolveCredentialFunc) (*ProvisionResponse, error) {
	names, err := resolveRequestedToolNames(s, req)
	if err != nil {
		return nil, err
	}
	out := &ProvisionResponse{MCPServers: map[string]MCPServerConfig{}}
	for _, name := range names {
		t, err := s.GetToolByName(name)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, fmt.Errorf("provision: tool %q not found", name)
			}
			return nil, fmt.Errorf("provision: lookup %q: %w", name, err)
		}
		if !t.Enabled {
			return nil, fmt.Errorf("provision: tool %q is disabled", name)
		}
		if t.Kind != KindMCP {
			return nil, fmt.Errorf("provision: tool %q has kind %q; only mcp tools are provisionable here", name, t.Kind)
		}
		if t.MCP == nil {
			return nil, fmt.Errorf("provision: tool %q has no mcp spec", name)
		}

		env, err := resolveEnv(ctx, t, resolve)
		if err != nil {
			return nil, err
		}

		cfg := MCPServerConfig{Env: env}
		switch t.MCP.Transport {
		case "stdio":
			cfg.Command = t.MCP.Command
			cfg.Args = t.MCP.Args
		case "http", "sse":
			cfg.URL = t.MCP.URL
			cfg.Type = t.MCP.Transport
		default:
			return nil, fmt.Errorf("provision: tool %q has unknown mcp transport %q", name, t.MCP.Transport)
		}
		out.MCPServers[name] = cfg
	}
	return out, nil
}

// resolveRequestedToolNames turns a ProvisionRequest into the list of tool
// names to build config for, rejecting a request that names neither source or
// both.
//
// The two paths deliberately treat a non-MCP tool differently. A caller that
// names tools outright gets an error from Provision if one of them is a CLI or
// an in-process local, because it asked for something this endpoint cannot
// hand back. An instance's opt-in list is not a provisioning request — it is a
// standing preference that spans every kind of tool the instance may use — so
// the MCP subset is selected out of it and the rest left alone. Erroring there
// would mean one ticked CLI tool wedges MCP provisioning for that instance.
func resolveRequestedToolNames(s *Store, req ProvisionRequest) ([]string, error) {
	switch {
	case len(req.Tools) > 0 && req.InstanceID != "":
		return nil, errors.New("provision: request has both tools and instance_id; pick one")
	case len(req.Tools) > 0:
		return req.Tools, nil
	case req.InstanceID != "":
		tools, err := s.ListInstanceTools(req.InstanceID)
		if err != nil {
			return nil, fmt.Errorf("provision: list tools for instance %q: %w", req.InstanceID, err)
		}
		var names []string
		for _, t := range tools {
			if t.Kind == KindMCP {
				names = append(names, t.Name)
			}
		}
		return names, nil
	default:
		return nil, errors.New("provision: at least one tool name or an instance_id is required")
	}
}

// resolveEnv walks a tool's EnvKeys and returns the map to embed in the MCP
// server config. Each env key MUST be mapped in t.Credentials to an
// auth-store provider; the resolver fetches the value. Missing mappings or
// resolver failures are errors — no silent fallback to os.Getenv.
func resolveEnv(ctx context.Context, t *Tool, resolve ResolveCredentialFunc) (map[string]string, error) {
	if len(t.EnvKeys) == 0 {
		return nil, nil
	}
	if resolve == nil {
		return nil, fmt.Errorf("provision: tool %q needs env keys %v but no credential resolver is configured", t.Name, t.EnvKeys)
	}
	out := make(map[string]string, len(t.EnvKeys))
	for _, k := range t.EnvKeys {
		provider, ok := t.Credentials[k]
		if !ok || provider == "" {
			return nil, fmt.Errorf("provision: tool %q env key %q has no credentials mapping", t.Name, k)
		}
		v, err := resolve(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("provision: tool %q resolve %q (provider %q): %w", t.Name, k, provider, err)
		}
		out[k] = v
	}
	return out, nil
}
