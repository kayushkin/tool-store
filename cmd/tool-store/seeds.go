package main

import (
	"errors"
	"fmt"

	toolstore "github.com/kayushkin/tool-store"
)

// seedMCPTools upserts a curated list of well-known MCP servers as kind=mcp
// rows. New rows default to enabled=false; updates preserve the user's enabled
// state. Idempotent on every restart — descriptions and launcher specs always
// refresh from this file (which is the canonical source for these entries).
func seedMCPTools(store *toolstore.Store) error {
	seeds := []toolstore.Tool{
		{
			Name:        "brave-search",
			DisplayName: "Brave Search",
			Description: "Web search via Brave Search API. Stdio MCP server published as @modelcontextprotocol/server-brave-search.",
			Kind:        toolstore.KindMCP,
			Tags:        []string{"search", "web"},
			EnvKeys:     []string{"BRAVE_API_KEY"},
			Credentials: map[string]string{"BRAVE_API_KEY": "brave"},
			MCP: &toolstore.MCPSpec{
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "@modelcontextprotocol/server-brave-search"},
			},
		},
		{
			Name:        "playwright",
			DisplayName: "Playwright",
			Description: "Browser automation via Microsoft's Playwright MCP server (acting: navigate/click/fill/snapshot, E2E, cross-browser). Stdio, headless.",
			Kind:        toolstore.KindMCP,
			Tags:        []string{"browser", "web", "frontend", "e2e"},
			MCP: &toolstore.MCPSpec{
				Transport: "stdio",
				Command:   "npx",
				// --headless: hosts here are display-less. --browser chromium: use the cached build.
				Args: []string{"-y", "@playwright/mcp@latest", "--headless", "--browser", "chromium"},
			},
		},
		{
			Name:        "chrome-devtools",
			DisplayName: "Chrome DevTools",
			Description: "Chrome DevTools MCP server (inspecting/debugging: performance traces & Web Vitals, console, network, CPU/network emulation). Stdio, headless, drives system Google Chrome.",
			Kind:        toolstore.KindMCP,
			Tags:        []string{"browser", "web", "frontend", "performance", "debug"},
			MCP: &toolstore.MCPSpec{
				Transport: "stdio",
				Command:   "npx",
				Args:      []string{"-y", "chrome-devtools-mcp@latest", "--headless"},
			},
		},
	}

	for _, t := range seeds {
		enabled := false
		if existing, err := store.GetToolByName(t.Name); err == nil {
			enabled = existing.Enabled
		} else if !errors.Is(err, toolstore.ErrNotFound) {
			return fmt.Errorf("lookup mcp tool %s: %w", t.Name, err)
		}
		t.Enabled = enabled
		if _, err := store.UpsertTool(&t); err != nil {
			return fmt.Errorf("upsert mcp tool %s: %w", t.Name, err)
		}
	}
	return nil
}
