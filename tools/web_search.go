package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	
	"github.com/kayushkin/tool-store/schema"
)

// WebSearch returns a tool that searches the web using the Brave Search API
// with braveAPIKey. The tool does not read the environment: whoever builds it
// supplies the key. With an empty key every search answers that the key is
// not set, naming the variable an operator would set.
func WebSearch(braveAPIKey string) Impl {
	type input struct {
		Query   string `json:"query"`
		Count   int    `json:"count"`
		Country string `json:"country"`
	}
	return Impl{
		Name:        "web_search",
		Description: "Search the web using Brave Search API. Returns titles, URLs, and snippets.",
		InputSchema: schema.Props([]string{"query"}, map[string]any{
			"query":   schema.Str("Search query string"),
			"count":   schema.Integer("Number of results (1-10, default 5)"),
			"country": schema.Str("2-letter country code (default US)"),
		}),
		Run: func(ctx context.Context, raw string) (string, error) {
			in, err := schema.Parse[input](raw)
			if err != nil {
				return "", err
			}

			apiKey := braveAPIKey
			if apiKey == "" {
				return "error: BRAVE_API_KEY environment variable not set", nil
			}

			count := in.Count
			if count <= 0 {
				count = 5
			}
			if count > 10 {
				count = 10
			}
			country := in.Country
			if country == "" {
				country = "US"
			}

			params := url.Values{}
			params.Set("q", in.Query)
			params.Set("count", fmt.Sprintf("%d", count))
			params.Set("country", country)

			req, err := http.NewRequestWithContext(ctx, "GET",
				"https://api.search.brave.com/res/v1/web/search?"+params.Encode(), nil)
			if err != nil {
				return fmt.Sprintf("error: %s", err), nil
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Accept-Encoding", "gzip")
			req.Header.Set("X-Subscription-Token", apiKey)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Sprintf("error: %s", err), nil
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Sprintf("error reading response: %s", err), nil
			}

			if resp.StatusCode != 200 {
				return fmt.Sprintf("error: Brave API returned %d: %s", resp.StatusCode, string(body)), nil
			}

			var result struct {
				Web struct {
					Results []struct {
						Title       string `json:"title"`
						URL         string `json:"url"`
						Description string `json:"description"`
					} `json:"results"`
				} `json:"web"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				return fmt.Sprintf("error parsing response: %s", err), nil
			}

			var sb strings.Builder
			for i, r := range result.Web.Results {
				if i > 0 {
					sb.WriteString("\n\n")
				}
				fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s", i+1, r.Title, r.URL, r.Description)
			}
			if sb.Len() == 0 {
				return "no results found", nil
			}
			return sb.String(), nil
		},
	}
}
