package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	
	"github.com/kayushkin/tool-store/schema"
)

func pinchtabURL() string {
	if u := os.Getenv("PINCHTAB_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:9867"
}

func pinchtabToken() string {
	return os.Getenv("PINCHTAB_TOKEN")
}

func pinchtabDo(method, path string, body any) ([]byte, error) {
	base := pinchtabURL()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, base+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tok := pinchtabToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Browser returns a tool that controls a browser via PinchTab's HTTP API.
func Browser() Impl {
	type input struct {
		Action string `json:"action"`
		URL    string `json:"url"`
		Ref    string `json:"ref"`
		Text   string `json:"text"`
		TabID  string `json:"tab_id"`
		Filter string `json:"filter"`
	}
	return Impl{
		Name:        "browser",
		Description: `Control a browser via PinchTab. Actions:
- navigate: go to a URL (requires "url")
- snapshot: get accessibility tree with element refs (e0, e1...). Use filter="interactive" for clickable elements only.
- click: click element by ref (requires "ref")
- type: type text into element (requires "ref" and "text")
- text: extract page text content
- screenshot: take a screenshot (returns base64 JPEG)
- tabs: list open tabs
- close: close a tab by tab_id`,
		InputSchema: schema.Props([]string{"action"}, map[string]any{
			"action": schema.Str(`Action: navigate, snapshot, click, type, text, screenshot, tabs, close`),
			"url":    schema.Str("URL to navigate to (for navigate action)"),
			"ref":    schema.Str("Element ref like e0, e5 (for click/type actions)"),
			"text":   schema.Str("Text to type (for type action)"),
			"tab_id": schema.Str("Tab ID (for close action)"),
			"filter": schema.Str("Snapshot filter: 'interactive' for clickable elements only"),
		}),
		Run: func(ctx context.Context, raw string) (string, error) {
			in, err := schema.Parse[input](raw)
			if err != nil {
				return "", err
			}

			switch in.Action {
			case "navigate":
				if in.URL == "" {
					return "error: url is required for navigate", nil
				}
				data, err := pinchtabDo("POST", "/navigate", map[string]string{"url": in.URL})
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "snapshot":
				path := "/snapshot"
				if in.Filter != "" {
					path += "?filter=" + in.Filter
				}
				data, err := pinchtabDo("GET", path, nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "click":
				if in.Ref == "" {
					return "error: ref is required for click", nil
				}
				data, err := pinchtabDo("POST", "/action", map[string]string{"kind": "click", "ref": in.Ref})
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "type":
				if in.Ref == "" {
					return "error: ref is required for type", nil
				}
				data, err := pinchtabDo("POST", "/action", map[string]string{"kind": "type", "ref": in.Ref, "text": in.Text})
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "text":
				data, err := pinchtabDo("GET", "/text", nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "screenshot":
				data, err := pinchtabDo("GET", "/screenshot", nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil

			case "tabs":
				instData, err := pinchtabDo("GET", "/instances", nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				var instances []struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(instData, &instances); err != nil {
					return fmt.Sprintf("error parsing instances: %s", err), nil
				}
				if len(instances) == 0 {
					return "no instances running", nil
				}
				data, err := pinchtabDo("GET", "/instances/"+instances[0].ID+"/tabs", nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			case "close":
				if in.TabID == "" {
					return "error: tab_id is required for close", nil
				}
				data, err := pinchtabDo("POST", "/tabs/"+in.TabID+"/close", nil)
				if err != nil {
					return fmt.Sprintf("error: %s", err), nil
				}
				return string(data), nil

			default:
				return fmt.Sprintf("error: unknown action %q. Use: navigate, snapshot, click, type, text, screenshot, tabs, close", in.Action), nil
			}
		},
	}
}
