package routerbridge

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// idleReader resets a watchdog on every successful read so only inactivity,
// not total duration, aborts a streaming response.
type idleReader struct {
	io.ReadCloser
	timer *time.Timer
	idle  time.Duration
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	return n, err
}

// Reset hints such as "Resets in 2h56m" make the IDE wait for the whole quota
// window before it tries again, so they are removed from the forwarded message.
// The unmodified text stays available through /health lastError.
var resetHint = regexp.MustCompile(`(?i)\s*(?:your quota )?(?:will )?(?:resets? (?:in|after)|retry (?:in|after)|try again in)\s+(?:\d+(?:\.\d+)?[hms]\s*)+\.?`)

func writeGoogleError(w http.ResponseWriter, code int, status, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message, "status": status}})
}

func googleStatus(code int) string {
	switch code {
	case 400:
		return "INVALID_ARGUMENT"
	case 401:
		return "UNAUTHENTICATED"
	case 403:
		return "PERMISSION_DENIED"
	case 404:
		return "NOT_FOUND"
	case 429:
		return "RESOURCE_EXHAUSTED"
	case 504:
		return "DEADLINE_EXCEEDED"
	}
	if code >= 500 {
		return "UNAVAILABLE"
	}
	return "UNKNOWN"
}

func routerErrorMessage(raw []byte, code int) string {
	var body struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		var nested struct{ Message string }
		if json.Unmarshal(body.Error, &nested) == nil && nested.Message != "" {
			return nested.Message
		}
		var text string
		if json.Unmarshal(body.Error, &text) == nil && text != "" {
			return text
		}
		if body.Message != "" {
			return body.Message
		}
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.HasPrefix(text, "<") {
		return fmt.Sprintf("router returned HTTP %d", code)
	}
	if len(text) > 300 {
		text = text[:300]
	}
	return text
}

// writeRouterError replaces a failed router response with a plain Google-style
// error. Retry-After and quota-reset details are dropped so a drained account
// pool fails fast instead of parking the IDE for hours.
func (b *Bridge) writeRouterError(w http.ResponseWriter, res *http.Response) {
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	message := routerErrorMessage(raw, res.StatusCode)
	b.mu.Lock()
	b.lastError = fmt.Sprintf("router %d: %s", res.StatusCode, message)
	b.mu.Unlock()
	message = strings.TrimSpace(resetHint.ReplaceAllString(message, ""))
	writeGoogleError(w, res.StatusCode, googleStatus(res.StatusCode), message)
}

func (b *Bridge) setLastError(message string) {
	b.mu.Lock()
	b.failures++
	b.lastError = message
	b.mu.Unlock()
}

type statusRecorder struct {
	http.ResponseWriter
	status, bytes int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytes += n
	return n, err
}

// Unwrap lets http.NewResponseController reach the underlying Flusher.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequest writes one line per proxied request to stderr (the private bridge
// log). It records only the Cloud Code method name, never the capability path,
// query, headers, or bodies.
func (b *Bridge) logRequest(r *http.Request, rec *statusRecorder, took time.Duration) {
	path := strings.TrimPrefix(r.URL.Path, "/"+b.opts.Capability+"/")
	if !strings.HasPrefix(path, "v1internal") {
		return
	}
	log.Printf("%s %s -> %d (%d bytes, %s)", r.Method, path, rec.status, rec.bytes, took.Round(time.Millisecond))
}
