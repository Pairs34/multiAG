package routerbridge

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failAfterReader struct {
	data []byte
	done bool
}

func (r *failAfterReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("synthetic stream interruption")
}

func (r *failAfterReader) Close() error { return nil }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const capability = "0123456789abcdef0123456789abcdef"

func TestGenerationUsesRouterCredentialAndPreservesToolPayload(t *testing.T) {
	payload := `{"model":"gemini-3.7-flash-low","request":{"contents":[{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"a.go"}}}]}]},"stream":true}`
	sse := "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read_file\",\"args\":{\"path\":\"a.go\"}}}]}}]}}\n\n"
	b, err := New(Options{RouterURL: "http://127.0.0.1:20128", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router-only", Capability: capability, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://127.0.0.1:20128/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer router-only" {
			t.Fatal("wrong target or credential")
		}
		if r.Header.Get("Cookie") != "" {
			t.Fatal("cookie leaked")
		}
		var got map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&got)
		if string(got["model"]) != `"ag/gemini-3.7-flash-low"` || got["stream"] != nil || !strings.Contains(string(got["request"]), "functionCall") {
			t.Fatal("payload damaged")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/"+capability+"/v1internal:streamGenerateContent?alt=sse", strings.NewReader(payload))
	r.Header.Set("Authorization", "Bearer ide-only")
	r.Header.Set("Cookie", "secret")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != sse {
		t.Fatal("SSE was not preserved")
	}
}

func TestMetadataKeepsIDEIdentityAndVerifiedGoogleTarget(t *testing.T) {
	b, err := New(Options{RouterURL: "http://127.0.0.1:20128", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router-only", Capability: capability, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist" || r.Header.Get("Authorization") != "Bearer ide-only" {
			t.Fatal("metadata credential or target incorrect")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/"+capability+"/v1internal:loadCodeAssist", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer ide-only")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestUntrustedRequestsDoNotReachUpstream(t *testing.T) {
	called := false
	b, _ := New(Options{RouterURL: "http://127.0.0.1:20128", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "key", Capability: capability, Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, nil })}})
	for _, test := range []struct{ path, body, origin string }{
		{"/v1internal:loadCodeAssist", `{}`, ""},
		{"/" + capability + "/v1internal:loadCodeAssist", `{}`, "https://untrusted.example"},
		{"/" + capability + "/v1internal:streamGenerateContent", `{} {}`, ""},
		{"/" + capability + "/v1internal:streamGenerateContent", `{"model":"x"} {}`, ""},
		{"/" + capability + "/https://evil.example", `{}`, ""},
	} {
		r := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code < 400 {
			t.Fatal("request accepted", test.path)
		}
	}
	if called {
		t.Fatal("untrusted request forwarded")
	}
}

func TestControlChangesRoutingWithoutRestart(t *testing.T) {
	var targets, auth []string
	b, _ := New(Options{RouterURL: "http://127.0.0.1:20128", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router", Capability: capability, Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		targets = append(targets, r.URL.Host)
		auth = append(auth, r.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}})
	for _, paused := range []bool{false, true, false} {
		control := httptest.NewRequest("POST", "http://127.0.0.1:20129/"+capability+"/control", strings.NewReader(`{"model":"ag/gemini-3.7-flash-low","paused":`+map[bool]string{true: "true", false: "false"}[paused]+`}`))
		control.Header.Set("Content-Type", "application/json")
		control.Header.Set("Origin", "http://127.0.0.1:20129")
		w := httptest.NewRecorder()
		b.ServeHTTP(w, control)
		if w.Code != 204 {
			t.Fatal("control failed", w.Code)
		}
		r := httptest.NewRequest("POST", "/"+capability+"/v1internal:streamGenerateContent", strings.NewReader(`{"model":"gemini-3.8-flash-high","request":{"contents":[]}}`))
		r.Header.Set("Authorization", "Bearer ide")
		w = httptest.NewRecorder()
		b.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if strings.Join(targets, ",") != "127.0.0.1:20128,cloudcode-pa.googleapis.com,127.0.0.1:20128" || strings.Join(auth, ",") != "Bearer router,Bearer ide,Bearer router" {
		t.Fatal("live mode switching failed", targets, auth)
	}
}

func TestRemoteRouterRequiresHTTPS(t *testing.T) {
	for _, address := range []string{"http://router.example.com", "http://192.0.2.1:8080"} {
		_, err := New(Options{RouterURL: address, UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "synthetic", Capability: capability})
		if err == nil {
			t.Fatalf("insecure remote router accepted: %s", address)
		}
	}
	for _, address := range []string{"https://router.example.com", "http://127.0.0.1:20128", "http://[::1]:20128"} {
		_, err := New(Options{RouterURL: address, UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "synthetic", Capability: capability})
		if err != nil {
			t.Fatalf("valid router rejected: %s: %v", address, err)
		}
	}
}

func TestInterruptedCompatibilityStreamEndsWithNativeCandidate(t *testing.T) {
	sse := []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
	b, err := New(Options{RouterURL: "https://router.example", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router", Capability: capability, WireFormat: "openai", Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &failAfterReader{data: sse}}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/"+capability+"/v1internal:streamGenerateContent", strings.NewReader(`{"model":"gemini-3.8-flash-high","request":{"contents":[{"role":"user","parts":[{"text":"test"}]}]}}`))
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), `data: {"error"`) || !strings.Contains(w.Body.String(), "Router stream interrupted") || !strings.Contains(w.Body.String(), `"finishReason":"STOP"`) {
		t.Fatal("stream was not terminated safely", w.Code, w.Body.String())
	}
}

func TestCompatibilityStreamFailureBeforeFirstFrameReturnsBadGateway(t *testing.T) {
	b, err := New(Options{RouterURL: "https://router.example", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router", Capability: capability, WireFormat: "openai", Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &failAfterReader{}}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/"+capability+"/v1internal:streamGenerateContent", strings.NewReader(`{"model":"gemini-3.8-flash-high","request":{"contents":[{"role":"user","parts":[{"text":"test"}]}]}}`))
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), `"status":"UNAVAILABLE"`) {
		t.Fatal("pre-stream failure was not returned as 502", w.Code, w.Body.String())
	}
}
