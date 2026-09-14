package routerbridge

import (
	_ "embed"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

//go:embed manage.html
var managementPage string

func (b *Bridge) manage(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "manage":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return true
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		io.WriteString(w, managementPage)
		return true
	case "control":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return true
		}
		if r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(415)
			return true
		}
		var control struct {
			Model  string `json:"model"`
			Paused bool   `json:"paused"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&control); err != nil {
			http.Error(w, "invalid control", 400)
			return true
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			http.Error(w, "invalid trailing JSON", 400)
			return true
		}
		if control.Model != "" && (!strings.HasPrefix(control.Model, "ag/") || strings.ContainsAny(control.Model, "\r\n\t ")) {
			http.Error(w, "select an Antigravity model", 400)
			return true
		}
		b.mu.Lock()
		b.model = control.Model
		b.paused = control.Paused
		b.mu.Unlock()
		w.WriteHeader(204)
		return true
	case "models":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return true
		}
		req, _ := http.NewRequestWithContext(r.Context(), "GET", b.opts.RouterURL+"/v1/models", nil)
		req.Header.Set("Authorization", "Bearer "+b.opts.APIKey)
		res, err := b.opts.Client.Do(req)
		if err != nil {
			http.Error(w, "router unavailable", 502)
			return true
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			http.Error(w, "router model list failed", 502)
			return true
		}
		var list struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&list); err != nil {
			http.Error(w, "invalid model list", 502)
			return true
		}
		models := []string{}
		for _, m := range list.Data {
			if strings.HasPrefix(m.ID, "ag/") {
				models = append(models, m.ID)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
		return true
	}
	return false
}
