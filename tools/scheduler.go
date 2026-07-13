package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	
	"github.com/kayushkin/tool-store/schema"
)

// Job represents a scheduler job (from scheduler/internal/db/db.go)
type Job struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Schedule      string     `json:"schedule"`      // cron expression
	Command       string     `json:"command"`       // shell command
	Type          string     `json:"type"`          // "shell" or "agent"
	Agent         string     `json:"agent,omitempty"`
	Prompt        string     `json:"prompt,omitempty"`
	Model         string     `json:"model,omitempty"`
	Orchestrator  string     `json:"orchestrator,omitempty"`   // "claude-code", "inber", etc.
	SessionID     string     `json:"session_id,omitempty"`     // session to resume
	Enabled       bool       `json:"enabled"`
	HoldUntil     *time.Time `json:"hold_until,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Run represents a job execution record
type Run struct {
	ID         int64      `json:"id"`
	JobID      int64      `json:"job_id"`
	Status     string     `json:"status"`
	Output     string     `json:"output"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// schedulerInput represents the input parameters for the scheduler tool
type schedulerInput struct {
	Action        string  `json:"action"`         // "list", "create", "get", "update", "delete", "runs"
	ID            *int64  `json:"id,omitempty"`   // job ID for get/update/delete/runs actions
	Name          *string `json:"name,omitempty"` // job name for create
	Schedule      *string `json:"schedule,omitempty"` // cron expression for create
	Command       *string `json:"command,omitempty"`  // shell command for create (type=shell)
	Type          *string `json:"type,omitempty"`     // "shell" or "agent" for create
	Agent         *string `json:"agent,omitempty"`    // agent name for create (type=agent)
	Prompt        *string `json:"prompt,omitempty"`   // prompt text for create (type=agent)
	Model         *string `json:"model,omitempty"`    // model override for create (type=agent)
	Orchestrator  *string `json:"orchestrator,omitempty"`   // "claude-code", "inber", etc. for create (type=agent)
	SessionID     *string `json:"session_id,omitempty"`     // session to resume for create (type=agent)
	Enabled       *bool   `json:"enabled,omitempty"`  // enable/disable for update
}

// Scheduler returns a tool that interacts with the scheduler HTTP API at localhost:8092.
func Scheduler() Impl {
	return Impl{
		Name:        "scheduler",
		Description: "Interact with the scheduler HTTP API to manage cron jobs. Supports listing, creating, updating, deleting jobs, and viewing run history.",
		InputSchema: schema.Props([]string{"action"}, map[string]any{
			"action":         schema.Str("Action to perform: list, create, get, update, delete, runs"),
			"id":             schema.Integer("Job ID (required for get, update, delete, runs actions)"),
			"name":           schema.Str("Job name (required for create)"),
			"schedule":       schema.Str("Cron expression (required for create)"),
			"command":        schema.Str("Shell command (required for create with type=shell)"),
			"type":           schema.Str("Job type: 'shell' or 'agent' (default: shell)"),
			"agent":          schema.Str("Agent name (required for create with type=agent)"),
			"prompt":         schema.Str("Prompt text (required for create with type=agent)"),
			"model":          schema.Str("Model override (optional for type=agent)"),
			"orchestrator":   schema.Str("Orchestrator name: 'claude-code', 'inber', etc. (default: claude-code)"),
			"session_id":     schema.Str("Session ID to resume (empty for new session)"),
			"enabled":        schema.Bool("Enable/disable job (for update action)"),
		}),
		Run: func(ctx context.Context, raw string) (string, error) {
			in, err := schema.Parse[schedulerInput](raw)
			if err != nil {
				return "", err
			}

			baseURL := os.Getenv("SCHEDULER_URL")
			if baseURL == "" {
				baseURL = "http://localhost:8092"
			}

			token := os.Getenv("SCHEDULER_TOKEN")
			
			switch in.Action {
			case "list":
				return handleList(ctx, baseURL, token)
			case "create":
				return handleCreate(ctx, baseURL, token, in)
			case "get":
				if in.ID == nil {
					return "error: id is required for get action", nil
				}
				return handleGet(ctx, baseURL, token, *in.ID)
			case "update":
				if in.ID == nil {
					return "error: id is required for update action", nil
				}
				return handleUpdate(ctx, baseURL, token, *in.ID, in)
			case "delete":
				if in.ID == nil {
					return "error: id is required for delete action", nil
				}
				return handleDelete(ctx, baseURL, token, *in.ID)
			case "runs":
				if in.ID == nil {
					return "error: id is required for runs action", nil
				}
				return handleRuns(ctx, baseURL, token, *in.ID)
			default:
				return "error: invalid action. Must be one of: list, create, get, update, delete, runs", nil
			}
		},
	}
}

func makeRequest(ctx context.Context, method, url, token string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return http.DefaultClient.Do(req)
}

func handleList(ctx context.Context, baseURL, token string) (string, error) {
	resp, err := makeRequest(ctx, "GET", baseURL+"/api/jobs", token, nil)
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error reading response: %s", err), nil
	}

	if resp.StatusCode != 200 {
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	var jobs []Job
	if err := json.Unmarshal(body, &jobs); err != nil {
		return fmt.Sprintf("error parsing response: %s", err), nil
	}

	if len(jobs) == 0 {
		return "no jobs found", nil
	}

	result := fmt.Sprintf("Found %d jobs:\n\n", len(jobs))
	for _, job := range jobs {
		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}
		if job.HoldUntil != nil && job.HoldUntil.After(time.Now()) {
			status += " (on hold)"
		}
		
		result += fmt.Sprintf("ID: %d\nName: %s\nSchedule: %s\nType: %s\nStatus: %s\n", 
			job.ID, job.Name, job.Schedule, job.Type, status)
		
		if job.Type == "shell" {
			result += fmt.Sprintf("Command: %s\n", job.Command)
		} else if job.Type == "agent" {
			result += fmt.Sprintf("Agent: %s\nPrompt: %s\n", job.Agent, job.Prompt)
			if job.Model != "" {
				result += fmt.Sprintf("Model: %s\n", job.Model)
			}
			if job.Orchestrator != "" {
				result += fmt.Sprintf("Orchestrator: %s\n", job.Orchestrator)
			}
			if job.SessionID != "" {
				result += fmt.Sprintf("Session ID: %s\n", job.SessionID)
			}
		}
		result += "\n"
	}

	return result, nil
}

func handleCreate(ctx context.Context, baseURL, token string, in schedulerInput) (string, error) {
	if in.Name == nil || in.Schedule == nil {
		return "error: name and schedule are required for create action", nil
	}

	jobType := "shell"
	if in.Type != nil {
		jobType = *in.Type
	}

	if jobType != "shell" && jobType != "agent" {
		return "error: type must be 'shell' or 'agent'", nil
	}

	if jobType == "shell" && (in.Command == nil || *in.Command == "") {
		return "error: command is required for shell jobs", nil
	}

	if jobType == "agent" && (in.Agent == nil || *in.Agent == "" || in.Prompt == nil || *in.Prompt == "") {
		return "error: agent and prompt are required for agent jobs", nil
	}

	reqBody := map[string]interface{}{
		"name":     *in.Name,
		"schedule": *in.Schedule,
		"type":     jobType,
	}

	if in.Command != nil {
		reqBody["command"] = *in.Command
	}
	if in.Agent != nil {
		reqBody["agent"] = *in.Agent
	}
	if in.Prompt != nil {
		reqBody["prompt"] = *in.Prompt
	}
	if in.Model != nil {
		reqBody["model"] = *in.Model
	}
	if in.Orchestrator != nil {
		reqBody["orchestrator"] = *in.Orchestrator
	}
	if in.SessionID != nil {
		reqBody["session_id"] = *in.SessionID
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Sprintf("error marshaling request: %s", err), nil
	}

	resp, err := makeRequest(ctx, "POST", baseURL+"/api/jobs", token, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error reading response: %s", err), nil
	}

	if resp.StatusCode != 201 {
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Sprintf("error parsing response: %s", err), nil
	}

	return fmt.Sprintf("Created job %d: %s\nSchedule: %s\nType: %s\nEnabled: %t", 
		job.ID, job.Name, job.Schedule, job.Type, job.Enabled), nil
}

func handleGet(ctx context.Context, baseURL, token string, id int64) (string, error) {
	url := fmt.Sprintf("%s/api/jobs/%d", baseURL, id)
	resp, err := makeRequest(ctx, "GET", url, token, nil)
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error reading response: %s", err), nil
	}

	if resp.StatusCode == 404 {
		return fmt.Sprintf("job %d not found", id), nil
	}
	if resp.StatusCode != 200 {
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Sprintf("error parsing response: %s", err), nil
	}

	result := fmt.Sprintf("Job %d:\nName: %s\nSchedule: %s\nType: %s\nEnabled: %t\n",
		job.ID, job.Name, job.Schedule, job.Type, job.Enabled)
	
	if job.HoldUntil != nil && job.HoldUntil.After(time.Now()) {
		result += fmt.Sprintf("On hold until: %s\n", job.HoldUntil.Format(time.RFC3339))
	}
	
	if job.Type == "shell" {
		result += fmt.Sprintf("Command: %s\n", job.Command)
	} else if job.Type == "agent" {
		result += fmt.Sprintf("Agent: %s\nPrompt: %s\n", job.Agent, job.Prompt)
		if job.Model != "" {
			result += fmt.Sprintf("Model: %s\n", job.Model)
		}
		if job.Orchestrator != "" {
			result += fmt.Sprintf("Orchestrator: %s\n", job.Orchestrator)
		}
		if job.SessionID != "" {
			result += fmt.Sprintf("Session ID: %s\n", job.SessionID)
		}
	}

	result += fmt.Sprintf("Created: %s\nUpdated: %s",
		job.CreatedAt.Format(time.RFC3339), job.UpdatedAt.Format(time.RFC3339))

	return result, nil
}

func handleUpdate(ctx context.Context, baseURL, token string, id int64, in schedulerInput) (string, error) {
	if in.Enabled == nil {
		return "error: currently only 'enabled' field can be updated", nil
	}

	reqBody := map[string]interface{}{
		"enabled": *in.Enabled,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Sprintf("error marshaling request: %s", err), nil
	}

	url := fmt.Sprintf("%s/api/jobs/%d", baseURL, id)
	resp, err := makeRequest(ctx, "PATCH", url, token, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error reading response: %s", err), nil
	}

	if resp.StatusCode == 404 {
		return fmt.Sprintf("job %d not found", id), nil
	}
	if resp.StatusCode != 200 {
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	var job Job
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Sprintf("error parsing response: %s", err), nil
	}

	action := "enabled"
	if !*in.Enabled {
		action = "disabled"
	}

	return fmt.Sprintf("Job %d (%s) %s successfully", job.ID, job.Name, action), nil
}

func handleDelete(ctx context.Context, baseURL, token string, id int64) (string, error) {
	url := fmt.Sprintf("%s/api/jobs/%d", baseURL, id)
	resp, err := makeRequest(ctx, "DELETE", url, token, nil)
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Sprintf("job %d not found", id), nil
	}
	if resp.StatusCode != 204 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	return fmt.Sprintf("Job %d deleted successfully", id), nil
}

func handleRuns(ctx context.Context, baseURL, token string, id int64) (string, error) {
	url := fmt.Sprintf("%s/api/jobs/%d/runs", baseURL, id)
	resp, err := makeRequest(ctx, "GET", url, token, nil)
	if err != nil {
		return fmt.Sprintf("error: %s", err), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("error reading response: %s", err), nil
	}

	if resp.StatusCode != 200 {
		return fmt.Sprintf("error: API returned %d: %s", resp.StatusCode, string(body)), nil
	}

	var runs []Run
	if err := json.Unmarshal(body, &runs); err != nil {
		return fmt.Sprintf("error parsing response: %s", err), nil
	}

	if len(runs) == 0 {
		return fmt.Sprintf("no runs found for job %d", id), nil
	}

	result := fmt.Sprintf("Found %d runs for job %d:\n\n", len(runs), id)
	for _, run := range runs {
		result += fmt.Sprintf("Run %d:\nStatus: %s\nStarted: %s\n", 
			run.ID, run.Status, run.StartedAt.Format(time.RFC3339))
		
		if run.FinishedAt != nil {
			result += fmt.Sprintf("Finished: %s\n", run.FinishedAt.Format(time.RFC3339))
		}
		
		if run.Output != "" {
			// Truncate output if too long
			output := run.Output
			if len(output) > 500 {
				output = output[:500] + "... (truncated)"
			}
			result += fmt.Sprintf("Output: %s\n", output)
		}
		result += "\n"
	}

	return result, nil
}