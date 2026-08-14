package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Every case here drives a NON-200. That is deliberate: the whole defect lived
// in the one line between "return the body" and "return an error", and a case
// that drives a 200 passes whether or not that line exists. Each non-200 case
// therefore has a 200 control beside it, so the assertions cannot be satisfied
// by a helper that simply fails on everything.

// pinchtabServing points the tool at a stub PinchTab and returns its URL.
func pinchtabServing(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	t.Setenv("PINCHTAB_URL", server.URL)
	t.Setenv("PINCHTAB_TOKEN", "")
}

// respondWith answers every request with one status and one body.
func respondWith(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func runBrowser(t *testing.T, arguments string) string {
	t.Helper()
	output, err := Browser().Run(context.Background(), arguments)
	if err != nil {
		t.Fatalf("the browser tool returned a transport error: %v", err)
	}
	return output
}

// The starkest case: a navigate PinchTab rejected used to read to the model as a
// navigation that worked, because the rejection body was returned as the result.
func TestNavigateReportsARejectionInsteadOfReturningItAsTheResult(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusNotFound, `{"error":"no tab to navigate"}`))

	output := runBrowser(t, `{"action":"navigate","url":"https://example.invalid"}`)

	if !strings.Contains(output, "404") {
		t.Errorf("a 404 from PinchTab did not reach the model as a failure: %q", output)
	}
	if !strings.HasPrefix(output, "error") {
		t.Errorf("PinchTab's error body was returned as the result of the action: %q", output)
	}
}

func TestNavigateStillReturnsTheBodyOnSuccess(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusOK, `{"ok":true}`))

	output := runBrowser(t, `{"action":"navigate","url":"https://example.invalid"}`)

	if output != `{"ok":true}` {
		t.Errorf("a successful navigate no longer returns PinchTab's body verbatim: %q", output)
	}
}

// A 2xx that is not 200 is still a success. Without this the status check could
// be written as `!= 200` and pass everything above.
func TestANonTwoHundredSuccessStatusIsStillASuccess(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusAccepted, `queued`))

	output := runBrowser(t, `{"action":"click","ref":"e3"}`)

	if output != "queued" {
		t.Errorf("202 Accepted was treated as a failure: %q", output)
	}
}

// The subtlest shape of the defect. `tabs` is the one action that unmarshals, so
// an error body that happens to be valid JSON decoded to zero instances — and
// the tool reported "no instances running", which is indistinguishable from a
// browser that is running with nothing open.
func TestTabsDoesNotReportAnErrorResponseAsAnEmptyBrowser(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusUnauthorized, `[]`))

	output := runBrowser(t, `{"action":"tabs"}`)

	if strings.Contains(output, "no instances running") {
		t.Errorf("a 401 was reported as a browser with no instances: %q", output)
	}
	if !strings.Contains(output, "401") {
		t.Errorf("the 401 never reached the model: %q", output)
	}
}

// ...and the reading it is confusable with has to keep working, or the
// assertion above could pass by never producing that message at all.
func TestTabsStillReportsAGenuinelyEmptyBrowser(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusOK, `[]`))

	output := runBrowser(t, `{"action":"tabs"}`)

	if output != "no instances running" {
		t.Errorf("an empty instance list on a 200 no longer reports an empty browser: %q", output)
	}
}

// The second request `tabs` makes is the one nothing was watching: the instance
// list can succeed and the per-instance tab fetch fail.
func TestTabsReportsAFailureOnTheSecondRequest(t *testing.T) {
	pinchtabServing(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/instances" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"i1"}]`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`instance went away`))
	})

	output := runBrowser(t, `{"action":"tabs"}`)

	if !strings.Contains(output, "500") {
		t.Errorf("a 500 on the tab list did not reach the model: %q", output)
	}
}

// screenshot was the case the card asked to be decided separately: it base64
// encodes whatever it is handed, so an error page became a valid-looking
// data:image/jpeg URL of the error text. Checking the status in the one helper
// answers it — the encoder is never reached with a body PinchTab did not stand
// behind.
func TestScreenshotDoesNotEncodeAnErrorPageAsAnImage(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusInternalServerError, `PinchTab crashed`))

	output := runBrowser(t, `{"action":"screenshot"}`)

	if strings.HasPrefix(output, "data:image/jpeg;base64,") {
		t.Errorf("an error page was handed to the model as an image: %q", output)
	}
	if !strings.Contains(output, "500") {
		t.Errorf("the 500 never reached the model: %q", output)
	}
}

func TestScreenshotStillEncodesARealScreenshot(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusOK, "\xff\xd8\xff jpeg bytes"))

	output := runBrowser(t, `{"action":"screenshot"}`)

	want := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("\xff\xd8\xff jpeg bytes"))
	if output != want {
		t.Errorf("a successful screenshot is no longer encoded: %q", output)
	}
}

// The status belongs to the error as a number, not only inside a formatted
// string, so a caller can branch on it without parsing prose.
func TestTheStatusIsRecoverableFromTheError(t *testing.T) {
	pinchtabServing(t, respondWith(http.StatusTeapot, `short and stout`))

	_, err := pinchtabRequestExpectingSuccess(context.Background(), "GET", "/text", nil)

	var status *pinchtabStatusError
	if !errors.As(err, &status) {
		t.Fatalf("a non-2xx did not produce a pinchtabStatusError: %v", err)
	}
	if status.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want 418", status.StatusCode)
	}
	if status.Body != "short and stout" {
		t.Errorf("the body PinchTab sent was dropped: %q", status.Body)
	}
}

// Cancelling a browser tool call used to cancel nothing: the request was built
// with http.NewRequest, so the caller's context reached neither the dial nor the
// wait for a response. navigate is the case under test because it is a POST —
// the branch that also builds a request body.
func TestCancellingABrowserCallStopsTheRequest(t *testing.T) {
	released := make(chan struct{})
	defer close(released)
	pinchtabServing(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-released:
		}
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan string, 1)
	go func() {
		output, _ := Browser().Run(ctx, `{"action":"navigate","url":"https://example.invalid"}`)
		finished <- output
	}()

	// Let the request actually reach the server, so this exercises abandoning an
	// in-flight request rather than refusing to start one.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case output := <-finished:
		if !strings.Contains(output, "context canceled") {
			t.Errorf("the call returned without reporting the cancellation: %q", output)
		}
	case <-time.After(cancellationDeadline):
		t.Fatal("the browser tool ignored its cancelled context and waited on the server")
	}
}
