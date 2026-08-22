package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// recordedRequest is what the stub saw. The stub asserts on the path rather
// than serving every URL alike: a path-blind fixture cannot notice a request
// sent to the wrong endpoint.
type recordedRequest struct {
	method string
	path   string
	body   string
	auth   string
	ctype  string
}

func pinchtabStub(t *testing.T, wantPath string, reply string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	got := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wantPath {
			// Reported, not served: a stub that answers every path alike would
			// let a wrong-URL bug through as a pass.
			t.Errorf("request path = %q, want %q", r.URL.Path, wantPath)
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		*got = recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(b),
			auth:   r.Header.Get("Authorization"),
			ctype:  r.Header.Get("Content-Type"),
		}
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func TestPinchtabDoSendsTheMethodAndPathItWasGiven(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs/snapshot", `{"ok":true}`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "")

	body, err := pinchtabDo("GET", "/tabs/snapshot", nil)
	if err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q, want the server's reply returned unchanged", body)
	}
	if got.method != "GET" {
		t.Errorf("method = %q, want GET", got.method)
	}
	if got.path != "/tabs/snapshot" {
		t.Errorf("path = %q, want /tabs/snapshot", got.path)
	}
}

func TestPinchtabDoMarshalsTheBodyAndSetsTheContentType(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs/navigate", `{}`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "")

	if _, err := pinchtabDo("POST", "/tabs/navigate", map[string]string{"url": "https://example.com"}); err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if got.method != "POST" {
		t.Errorf("method = %q, want POST", got.method)
	}
	var sent map[string]string
	if err := json.Unmarshal([]byte(got.body), &sent); err != nil {
		t.Fatalf("body %q is not the JSON the caller passed: %v", got.body, err)
	}
	if sent["url"] != "https://example.com" {
		t.Errorf("sent body = %v, want the caller's url", sent)
	}
	if got.ctype != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got.ctype)
	}
}

// A nil body must send no body and no Content-Type — the header is set only
// alongside a payload.
func TestPinchtabDoSendsNoContentTypeWithoutABody(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs", `[]`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "")

	if _, err := pinchtabDo("GET", "/tabs", nil); err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if got.body != "" {
		t.Errorf("body = %q, want empty", got.body)
	}
	if got.ctype != "" {
		t.Errorf("Content-Type = %q, want it unset when there is no body", got.ctype)
	}
}

func TestPinchtabDoSendsTheBearerTokenWhenOneIsSet(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs", `[]`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "s3cret")

	if _, err := pinchtabDo("GET", "/tabs", nil); err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if got.auth != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want %q", got.auth, "Bearer s3cret")
	}
}

// The negative case matters on its own: an unset token must send no header at
// all rather than an empty bearer, which some servers accept as a credential.
func TestPinchtabDoSendsNoAuthorizationHeaderWithoutAToken(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs", `[]`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "")

	if _, err := pinchtabDo("GET", "/tabs", nil); err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if got.auth != "" {
		t.Errorf("Authorization = %q, want no header when no token is set", got.auth)
	}
}

// PINCHTAB_URL is documented as tolerating a trailing slash, so the joined URL
// must not end up with a doubled one.
func TestPinchtabDoTrimsATrailingSlashFromTheBaseURL(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs", `[]`)
	t.Setenv("PINCHTAB_URL", srv.URL+"/")
	t.Setenv("PINCHTAB_TOKEN", "")

	if _, err := pinchtabDo("GET", "/tabs", nil); err != nil {
		t.Fatalf("pinchtabDo: %v", err)
	}

	if got.path != "/tabs" {
		t.Errorf("path = %q, want /tabs and not a doubled slash", got.path)
	}
}

// A transport failure must reach the caller. The engine surfaces it as a tool
// error, so swallowing it here would read to the model as an empty page.
func TestPinchtabDoReturnsTheTransportError(t *testing.T) {
	srv, _ := pinchtabStub(t, "/tabs", `[]`)
	url := srv.URL
	srv.Close() // nothing is listening now
	t.Setenv("PINCHTAB_URL", url)
	t.Setenv("PINCHTAB_TOKEN", "")

	body, err := pinchtabDo("GET", "/tabs", nil)
	if err == nil {
		t.Fatalf("no error from an unreachable server, got body %q", body)
	}
	if body != nil {
		t.Errorf("body = %q, want nil alongside the error", body)
	}
}

// An unmarshalable body must fail before any request is made, rather than
// sending a half-formed one.
func TestPinchtabDoRefusesABodyItCannotMarshal(t *testing.T) {
	srv, got := pinchtabStub(t, "/tabs", `[]`)
	t.Setenv("PINCHTAB_URL", srv.URL)
	t.Setenv("PINCHTAB_TOKEN", "")

	if _, err := pinchtabDo("POST", "/tabs", make(chan int)); err == nil {
		t.Fatal("a channel was accepted as a JSON body, want an error")
	}
	if got.method != "" {
		t.Errorf("a request was sent anyway: %+v", got)
	}
}
