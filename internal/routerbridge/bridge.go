package routerbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Options contains runtime credentials; callers must not log them.
type Options struct {
	RouterURL, UpstreamURL, APIKey, Capability, Model, WireFormat string
	Client                                                        *http.Client
}

type Bridge struct {
	opts                           Options
	mu                             sync.Mutex
	generation, metadata, failures uint64
	lastModel                      string
	model                          string
	paused                         bool
	lastError                      string
}

func New(opts Options) (*Bridge, error) {
	if opts.WireFormat != "" && opts.WireFormat != "native" && opts.WireFormat != "openai" {
		return nil, errors.New("unsupported router wire format")
	}
	r, err := url.Parse(opts.RouterURL)
	if err != nil || r.User != nil || r.RawQuery != "" || r.Fragment != "" || (r.Scheme != "http" && r.Scheme != "https") || r.Host == "" {
		return nil, errors.New("invalid router URL")
	}
	if ip := net.ParseIP(r.Hostname()); r.Scheme == "http" && r.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("remote routers require HTTPS")
	}
	u, err := url.Parse(opts.UpstreamURL)
	if err != nil || u.Scheme != "https" || (u.Host != "daily-cloudcode-pa.googleapis.com" && u.Host != "cloudcode-pa.googleapis.com") || u.Path != "" || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("upstream must be an HTTPS Google Cloud Code host")
	}
	if opts.APIKey == "" || len(opts.Capability) < 32 || strings.ContainsAny(opts.Capability, "/?#") {
		return nil, errors.New("API key and a long URL capability are required")
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	opts.RouterURL = strings.TrimRight(opts.RouterURL, "/")
	return &Bridge{opts: opts, model: opts.Model}, nil
}

func (b *Bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// A random base path also prevents websites from using this local bridge.
	prefix := "/" + b.opts.Capability + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, prefix)
	if origin := r.Header.Get("Origin"); origin != "" && (path != "control" || origin != "http://"+r.Host) {
		http.NotFound(w, r)
		return
	}
	if b.manage(w, r, path) {
		return
	}
	if path == "health" && r.Method == "GET" {
		b.mu.Lock()
		defer b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"generation": b.generation, "metadata": b.metadata, "failures": b.failures, "lastModel": b.lastModel, "model": b.model, "paused": b.paused, "router": b.opts.RouterURL, "lastError": b.lastError})
		return
	}
	if !strings.HasPrefix(path, "v1internal:") && !strings.HasPrefix(path, "v1internal/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	paused, override := b.paused, b.model
	b.mu.Unlock()
	isGeneration := !paused && (path == "v1internal:streamGenerateContent" || path == "v1internal:generateContent")
	var body io.Reader = r.Body
	target := b.opts.UpstreamURL + "/" + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	model := ""
	if isGeneration {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20))
		if err := dec.Decode(&payload); err != nil || payload == nil {
			http.Error(w, "invalid generation JSON", 400)
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid trailing JSON", 400)
			return
		}
		json.Unmarshal(payload["model"], &model)
		if model == "" {
			http.Error(w, "missing model", 400)
			return
		}
		// Tab completion is kept on the IDE account: router support differs.
		if !strings.HasPrefix(model, "tab_") && !strings.HasPrefix(model, "tab-") {
			if override != "" {
				model = override
			} else if !strings.HasPrefix(model, "ag/") {
				model = "ag/" + model
			}
			payload["model"], _ = json.Marshal(model)
			payload["userAgent"] = json.RawMessage(`"antigravity"`)
			// The Cloud Code wire schema has no OpenAI stream field.
			delete(payload, "stream")
			encoded, err := json.Marshal(payload)
			if b.opts.WireFormat == "openai" {
				encoded, err = nativeToOpenAI(payload)
			}
			if err != nil {
				b.mu.Lock()
				b.failures++
				b.lastError = err.Error()
				b.mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 400, "message": err.Error(), "status": "INVALID_ARGUMENT"}})
				return
			}
			body = bytes.NewReader(encoded)
			target = b.opts.RouterURL + "/v1/chat/completions"
		} else {
			encoded, _ := json.Marshal(payload)
			body = bytes.NewReader(encoded)
			isGeneration = false
		}
	}
	out, err := http.NewRequestWithContext(r.Context(), r.Method, target, body)
	if err != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	// Only explicit headers are forwarded. IDE OAuth never reaches the router.
	for _, key := range []string{"Content-Type", "Accept", "User-Agent"} {
		if value := r.Header.Get(key); value != "" {
			out.Header.Set(key, value)
		}
	}
	if isGeneration {
		out.Header.Set("Authorization", "Bearer "+b.opts.APIKey)
		out.Header.Set("Content-Type", "application/json")
	} else {
		for _, key := range []string{"Authorization", "X-Goog-User-Project", "X-Goog-Api-Client"} {
			if value := r.Header.Get(key); value != "" {
				out.Header.Set(key, value)
			}
		}
	}
	res, err := b.opts.Client.Do(out)
	if err != nil {
		b.mu.Lock()
		b.failures++
		b.mu.Unlock()
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	if isGeneration && b.opts.WireFormat == "openai" && res.StatusCode < 400 {
		if !strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
			http.Error(w, "expected router event stream", 502)
			return
		}
		if path == "v1internal:generateContent" {
			var converted bytes.Buffer
			err := openAIToNative(res.Body, func(frame []byte) error {
				if converted.Len()+len(frame) > 32<<20 {
					return errors.New("response too large")
				}
				converted.WriteString("data: ")
				converted.Write(frame)
				converted.WriteString("\n\n")
				return nil
			})
			if err != nil {
				b.mu.Lock()
				b.failures++
				b.mu.Unlock()
				http.Error(w, "invalid router stream", 502)
				return
			}
			res.Body = io.NopCloser(bytes.NewReader(converted.Bytes()))
		}
	}
	if isGeneration && path == "v1internal:generateContent" && strings.Contains(res.Header.Get("Content-Type"), "text/event-stream") {
		encoded, err := collectGeneration(res.Body)
		if err != nil {
			b.mu.Lock()
			b.failures++
			b.mu.Unlock()
			http.Error(w, "invalid router generation stream", http.StatusBadGateway)
			return
		}
		res.Body = io.NopCloser(bytes.NewReader(encoded))
		res.Header.Set("Content-Type", "application/json")
	}
	b.mu.Lock()
	if isGeneration {
		b.generation++
		b.lastModel = model
	} else {
		b.metadata++
	}
	if res.StatusCode >= 400 {
		b.failures++
	}
	b.mu.Unlock()
	for _, key := range []string{"Content-Type", "Retry-After"} {
		if v := res.Header.Get(key); v != "" {
			w.Header().Set(key, v)
		}
	}
	if isGeneration && b.opts.WireFormat == "openai" && path == "v1internal:streamGenerateContent" && res.StatusCode < 400 {
		wroteFrame := false
		err := openAIToNativeStream(res.Body, func(frame []byte) error {
			if !wroteFrame {
				w.WriteHeader(res.StatusCode)
				wroteFrame = true
			}
			if _, err := w.Write(append(append([]byte("data: "), frame...), []byte("\n\n")...)); err != nil {
				return err
			}
			return http.NewResponseController(w).Flush()
		})
		if err != nil {
			b.mu.Lock()
			b.failures++
			b.lastError = err.Error()
			b.mu.Unlock()
			if !wroteFrame {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": http.StatusBadGateway, "message": "router stream failed before producing output", "status": "UNAVAILABLE"}})
				return
			}
			// Once streaming has started it is too late for an HTTP error or safe
			// account retry. A top-level error frame crashes affected IDE versions,
			// so finish with a valid native candidate that leaves the editor usable.
			terminal, _ := json.Marshal(map[string]any{"response": map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": "\n\nRouter stream interrupted. Please retry."}}}, "finishReason": "STOP"}}}})
			io.WriteString(w, "data: "+string(terminal)+"\n\n")
			http.NewResponseController(w).Flush()
		}
		return
	}
	w.WriteHeader(res.StatusCode)
	// Flush each read so streaming does not wait for the entire response.
	buf := make([]byte, 16<<10)
	for {
		n, readErr := res.Body.Read(buf)
		if n > 0 {
			if _, err = w.Write(buf[:n]); err != nil {
				return
			}
			http.NewResponseController(w).Flush()
		}
		if readErr != nil {
			return
		}
	}
}
