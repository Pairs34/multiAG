package routerbridge

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Cloud Code's non-streaming method expects JSON even when 9Router returns SSE.
func collectGeneration(reader io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(io.LimitReader(reader, (32<<20)+1))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	response := map[string]any{}
	candidates := []any{}
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > 32<<20 {
			return nil, errors.New("generation too large")
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var frame struct {
			Response map[string]any  `json:"response"`
			Error    json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			return nil, err
		}
		if frame.Error != nil || frame.Response == nil {
			return nil, errors.New("router stream error")
		}
		for key, value := range frame.Response {
			if key != "candidates" {
				response[key] = value
			}
		}
		incoming, _ := frame.Response["candidates"].([]any)
		for i, entry := range incoming {
			candidate, ok := entry.(map[string]any)
			if !ok {
				return nil, errors.New("invalid candidate")
			}
			if i >= len(candidates) {
				candidates = append(candidates, candidate)
				continue
			}
			previous := candidates[i].(map[string]any)
			for key, value := range candidate {
				if key == "content" {
					content, ok := value.(map[string]any)
					if !ok {
						return nil, errors.New("invalid content")
					}
					old, ok := previous[key].(map[string]any)
					if !ok {
						previous[key] = content
						continue
					}
					parts, _ := old["parts"].([]any)
					next, _ := content["parts"].([]any)
					for k, v := range content {
						old[k] = v
					}
					old["parts"] = append(parts, next...)
				} else {
					previous[key] = value
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errors.New("empty generation")
	}
	response["candidates"] = candidates
	return json.Marshal(map[string]any{"response": response})
}
