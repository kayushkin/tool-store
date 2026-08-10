package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The scheduler does not refuse a field it will not apply. It answers 200 and
// returns the row unchanged, so the only evidence of a dropped edit is the job
// in the reply. Measured live 2026-08-10 against the binary serving :8092:
//
//	PATCH /api/jobs/50 {"schedule":"0 6 * * *","command":"echo CHANGED"} -> 200
//
// with both fields unchanged in the echoed row and on re-read. The tool used to
// print "updated: schedule, command" for exactly that exchange.
//
// Which fields a real scheduler honours depends on which build is serving the
// port — HEAD takes all thirteen, older binaries take only the three below — so
// these tests drive both shapes rather than picking one and calling it the
// truth. Nothing here needs to know which binary is deployed, which is the
// point of reading the echo instead of the request.
var fieldsTheOlderSchedulerHonours = []string{"enabled", "description", "timeout_seconds"}

// fakeScheduler serves PATCH /api/jobs/{id} over a job it holds, applying only
// the fields it was told to honour and silently keeping its own value for the
// rest — which is what the deployed scheduler does.
func fakeScheduler(t *testing.T, job Job, honoured []string) (baseURL string, lastRequestBody func() map[string]any) {
	t.Helper()

	honours := map[string]bool{}
	for _, name := range honoured {
		honours[name] = true
	}

	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("fake scheduler got %s, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("fake scheduler could not decode the request body: %s", err)
		}

		for name, value := range received {
			if !honours[name] {
				continue
			}
			switch name {
			case "name":
				job.Name = value.(string)
			case "description":
				job.Description = value.(string)
			case "schedule":
				job.Schedule = value.(string)
			case "command":
				job.Command = value.(string)
			case "prompt":
				job.Prompt = value.(string)
			case "timeout_seconds":
				job.TimeoutSecs = int(value.(float64))
			case "enabled":
				job.Enabled = value.(bool)
			default:
				t.Errorf("fake scheduler was told to honour %q but does not know how", name)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(job); err != nil {
			t.Errorf("fake scheduler could not encode its reply: %s", err)
		}
	}))
	t.Cleanup(server.Close)

	return server.URL, func() map[string]any { return received }
}

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }

// A job whose stored values differ from everything the tests ask for, so that
// "unchanged" and "applied" can never be confused for one another.
func jobBeforeUpdate() Job {
	return Job{
		ID:          50,
		Name:        "nightly thing",
		Schedule:    "0 0 31 2 *",
		Command:     "echo original",
		Description: "as filed",
		Type:        "shell",
		Enabled:     true,
	}
}

func TestUpdateReportsEveryFieldAppliedWhenTheServerAppliesThemAll(t *testing.T) {
	everyField := []string{"name", "description", "schedule", "command", "prompt", "timeout_seconds", "enabled"}
	baseURL, _ := fakeScheduler(t, jobBeforeUpdate(), everyField)

	report, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		Schedule: stringPointer("0 6 * * *"),
		Command:  stringPointer("echo CHANGED"),
	})
	if err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	if !strings.Contains(report, "updated: schedule, command") {
		t.Errorf("a server that applied both fields was not reported as having applied both:\n%s", report)
	}
	if strings.Contains(report, "NOT applied") {
		t.Errorf("a server that applied both fields was reported as dropping one:\n%s", report)
	}
}

// The defect this card was filed for. Against the deployed scheduler these two
// fields are dropped, and the tool announced them as updated.
func TestUpdateDoesNotReportAFieldTheServerSilentlyDropped(t *testing.T) {
	baseURL, _ := fakeScheduler(t, jobBeforeUpdate(), fieldsTheOlderSchedulerHonours)

	report, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		Schedule: stringPointer("0 6 * * *"),
		Command:  stringPointer("echo CHANGED"),
	})
	if err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	if strings.Contains(report, "updated: schedule, command") {
		t.Errorf("the tool announced two edits the server never applied:\n%s", report)
	}
	if !strings.Contains(report, "changed NOTHING") {
		t.Errorf("an update that applied no field at all did not say so:\n%s", report)
	}
	for _, want := range []string{"schedule", "command", `"0 6 * * *"`, `"0 0 31 2 *"`} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not mention %s, so the caller cannot see what was dropped:\n%s", want, report)
		}
	}
}

// The mixed case is the one a report has to get right in both directions at
// once: enabled lands, schedule does not, and saying either about the other is
// a lie.
func TestUpdateSeparatesTheAppliedFieldsFromTheDroppedOnes(t *testing.T) {
	baseURL, _ := fakeScheduler(t, jobBeforeUpdate(), fieldsTheOlderSchedulerHonours)

	report, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		Enabled:  boolPointer(false),
		Schedule: stringPointer("0 6 * * *"),
	})
	if err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	if !strings.Contains(report, "PARTIALLY applied") {
		t.Errorf("an update that landed one field of two did not report itself as partial:\n%s", report)
	}
	if !strings.Contains(report, "Applied: enabled") {
		t.Errorf("enabled was applied by the server and the report does not say so:\n%s", report)
	}
	appliedSection, droppedSection, found := strings.Cut(report, "NOT applied:")
	if !found {
		t.Fatalf("no NOT applied section in a report with a dropped field:\n%s", report)
	}
	if !strings.Contains(droppedSection, "schedule") {
		t.Errorf("schedule was dropped and is not in the NOT applied section:\n%s", report)
	}
	if strings.Contains(droppedSection, "enabled") {
		t.Errorf("enabled was applied but appears in the NOT applied section:\n%s", report)
	}
	if strings.Contains(appliedSection, "Applied: enabled, schedule") {
		t.Errorf("schedule was dropped but is listed as applied:\n%s", report)
	}
}

// Reading the echo only works if the request carried the field in the first
// place. This is the half b21b4bb fixed, and it stays pinned so a later change
// cannot go back to sending nothing and reporting the echo as agreement.
func TestUpdateForwardsEveryFieldTheCallerNamed(t *testing.T) {
	baseURL, lastRequestBody := fakeScheduler(t, jobBeforeUpdate(), fieldsTheOlderSchedulerHonours)

	if _, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		Schedule: stringPointer("0 6 * * *"),
		Prompt:   stringPointer("summarise yesterday"),
	}); err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	sent := lastRequestBody()
	for field, want := range map[string]string{"schedule": "0 6 * * *", "prompt": "summarise yesterday"} {
		if got, present := sent[field]; !present {
			t.Errorf("the request never carried %q", field)
		} else if got != want {
			t.Errorf("the request sent %q for %q, want %q", got, field, want)
		}
	}
	if _, present := sent["command"]; present {
		t.Errorf("the request carried command, which the caller never named: %v", sent)
	}
}

// Every other case here compares a string or a bool. timeout_seconds is the
// only number among the thirteen, and the comparison is == on an `any`, which
// is type-sensitive: the value asked for arrives as an int while a number
// decoded straight out of JSON is a float64. If those two ever meet, an applied
// field reports itself as dropped forever and no string case can see it.
func TestUpdateComparesANumericFieldByValueAndNotByItsJSONType(t *testing.T) {
	timeout := 1800
	baseURL, _ := fakeScheduler(t, jobBeforeUpdate(), fieldsTheOlderSchedulerHonours)

	report, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		TimeoutSecs: &timeout,
	})
	if err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	if !strings.Contains(report, "updated: timeout_seconds") {
		t.Errorf("the server applied timeout_seconds and the report denies it:\n%s", report)
	}
}

// An unchanged field is not a dropped one. Asking for the value a job already
// holds is a no-op the server applies by definition, and reporting it as
// refused would make every idempotent update look broken.
func TestUpdateCountsAFieldSetToItsCurrentValueAsApplied(t *testing.T) {
	baseURL, _ := fakeScheduler(t, jobBeforeUpdate(), fieldsTheOlderSchedulerHonours)

	report, err := handleUpdate(context.Background(), baseURL, "", 50, schedulerInput{
		Schedule: stringPointer("0 0 31 2 *"),
	})
	if err != nil {
		t.Fatalf("handleUpdate returned an error: %s", err)
	}

	if !strings.Contains(report, "updated: schedule") {
		t.Errorf("asking for the value the job already holds was not reported as applied:\n%s", report)
	}
}
