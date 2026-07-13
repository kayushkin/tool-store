package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// resolveFromAuthStore returns a credential resolver that calls auth-store's
// HTTP API (canonical credential service). The resolver is invoked from
// /provision when an MCP tool needs an env var resolved.
//
// Configuration:
//   AUTH_STORE_URL    base URL of auth-store (default http://127.0.0.1:8303)
//   AUTH_STORE_TOKEN  bearer token (matches auth-store's AUTHSTORE_TOKEN env)
//
// Per the single-source-of-truth directive, missing creds fail loudly — no
// env-var or other fallback.
func resolveFromAuthStore() func(ctx context.Context, provider string) (string, error) {
	base := strings.TrimRight(getenv("AUTH_STORE_URL", "http://127.0.0.1:8303"), "/")
	token := os.Getenv("AUTH_STORE_TOKEN")

	client := &http.Client{Timeout: 10 * time.Second}

	return func(ctx context.Context, provider string) (string, error) {
		if provider == "" {
			return "", errors.New("auth-store: empty provider")
		}
		u := fmt.Sprintf("%s/api/resolve/%s", base, url.PathEscape(provider))
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return "", fmt.Errorf("auth-store: build request: %w", err)
		}
		req.Header.Set("X-Auth-App", "tool-store")
		req.Header.Set("X-Auth-Reason", "provision:"+provider)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("auth-store: %w", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return "", fmt.Errorf("auth-store: provider %q resolve returned %d: %s", provider, resp.StatusCode, body)
		}
		var out struct {
			AuthType    string `json:"auth_type"`
			AccessToken string `json:"access_token"`
			APIKey      string `json:"api_key"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return "", fmt.Errorf("auth-store: parse: %w", err)
		}
		switch out.AuthType {
		case "api_key":
			if out.APIKey == "" {
				return "", fmt.Errorf("auth-store: provider %q has empty api_key", provider)
			}
			return out.APIKey, nil
		case "oauth", "token":
			if out.AccessToken == "" {
				return "", fmt.Errorf("auth-store: provider %q has empty access_token", provider)
			}
			return out.AccessToken, nil
		default:
			return "", fmt.Errorf("auth-store: provider %q has unsupported auth_type %q", provider, out.AuthType)
		}
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
