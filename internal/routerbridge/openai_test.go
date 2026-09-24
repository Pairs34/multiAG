package routerbridge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompatibilityRequestPreservesImagesAndRepeatedToolResults(t *testing.T) {
	input := `{"model":"ag/gemini-3.8-flash-high","request":{"systemInstruction":{"parts":[{"text":"system"}]},"generationConfig":{"maxOutputTokens":256},"contents":[{"role":"user","parts":[{"text":"test"},{"inlineData":{"mimeType":"image/png","data":"YQ=="}}]},{"role":"model","parts":[{"functionCall":{"name":"read","args":{"path":"a"}}},{"functionCall":{"name":"read","args":{"path":"b"}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"result":"a"}}},{"functionResponse":{"name":"read","response":{"result":"b"}}}]}],"tools":[{"functionDeclarations":[{"name":"read","parameters":{"type":"OBJECT","properties":{"path":{"type":"STRING"}}}}]}]}}`
	var payload map[string]json.RawMessage
	json.Unmarshal([]byte(input), &payload)
	encoded, err := nativeToOpenAI(payload)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Model    string
		Stream   bool
		Messages []struct {
			Role       string
			Content    json.RawMessage
			ToolCallID string                `json:"tool_call_id"`
			ToolCalls  []struct{ ID string } `json:"tool_calls"`
		}
		Tools     json.RawMessage
		MaxTokens int `json:"max_tokens"`
	}
	json.Unmarshal(encoded, &result)
	if result.Model != "ag/gemini-3.8-flash-high" || !result.Stream || result.MaxTokens != 256 || len(result.Messages) != 5 {
		t.Fatal("wrong request")
	}
	if result.Messages[2].ToolCalls[0].ID == result.Messages[2].ToolCalls[1].ID || result.Messages[3].ToolCallID != result.Messages[2].ToolCalls[0].ID || result.Messages[4].ToolCallID != result.Messages[2].ToolCalls[1].ID {
		t.Fatal("tool results paired incorrectly")
	}
	if !strings.Contains(string(result.Messages[1].Content), "data:image/png;base64,YQ==") || strings.Contains(string(result.Tools), "OBJECT") || !strings.Contains(string(result.Tools), "string") {
		t.Fatal("image or schema damaged")
	}
}

func TestCompatibilityRequestAcceptsSnakeCaseInlineImage(t *testing.T) {
	var payload map[string]json.RawMessage
	json.Unmarshal([]byte(`{"model":"ag/gemini-3.8-flash-high","request":{"contents":[{"role":"user","parts":[{"inline_data":{"mime_type":"image/png","data":"YQ=="}}]}]}}`), &payload)
	encoded, err := nativeToOpenAI(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "data:image/png;base64,YQ==") {
		t.Fatal("snake_case inline image was not preserved")
	}
}

const openAIStream = `data: {"id":"r1","model":"gemini-3.8-flash-high","choices":[{"index":0,"delta":{"reasoning_content":"thought","tool_calls":[{"index":0,"function":{"name":"pwd","arguments":"{\"x\":"}}]}}]}

data: {"id":"r1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}

data: {"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}

data: [DONE]
`

func TestCompatibilityStreamEmitsCompleteToolsAndUsage(t *testing.T) {
	frames := []map[string]any{}
	err := openAIToNativeStream(strings.NewReader(openAIStream), func(data []byte) error {
		var frame map[string]any
		json.Unmarshal(data, &frame)
		frames = append(frames, frame)
		return nil
	})
	if err != nil || len(frames) != 2 {
		t.Fatal("bad stream", err, len(frames))
	}
	first := frames[0]["response"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)
	if len(first) != 1 || first[0].(map[string]any)["thought"] != true {
		t.Fatal("tool emitted before arguments completed")
	}
	second := frames[1]["response"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["content"].(map[string]any)["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if second["name"] != "pwd" || second["args"].(map[string]any)["x"] != float64(1) {
		t.Fatal("tool call damaged")
	}
	for _, frame := range frames {
		response := frame["response"].(map[string]any)
		candidates, ok := response["candidates"].([]any)
		if !ok || len(candidates) == 0 {
			t.Fatal("candidate-less frame emitted")
		}
	}
}

func TestCompatibilityNonstreamHandlerReturnsNativeJSON(t *testing.T) {
	b, err := New(Options{RouterURL: "https://router.example", UpstreamURL: "https://cloudcode-pa.googleapis.com", APIKey: "router", Capability: capability, WireFormat: "openai", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer router" {
			t.Fatal("IDE credential leaked")
		}
		var payload map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["messages"] == nil || payload["request"] != nil {
			t.Fatal("native request sent in compatibility mode")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(openAIStream))}, nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/"+capability+"/v1internal:generateContent", strings.NewReader(`{"model":"gemini-3.8-flash-high","request":{"contents":[{"role":"user","parts":[{"text":"test"}]}]}}`))
	r.Header.Set("Authorization", "Bearer IDE")
	w := httptest.NewRecorder()
	b.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("wrong response", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["response"].(map[string]any)["usageMetadata"].(map[string]any)["totalTokenCount"] != float64(5) {
		t.Fatal("native JSON usage lost")
	}
}

func TestCompatibilityRejectsIncompleteToolArguments(t *testing.T) {
	input := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"dangerous","arguments":"{\"incomplete\":"}}]},"finish_reason":"tool_calls"}]}`
	if err := openAIToNative(strings.NewReader(input), func([]byte) error { t.Fatal("partial tool call emitted"); return nil }); err == nil {
		t.Fatal("partial tool arguments accepted")
	}
}

func TestCompatibilityAcceptsValidatedToolMode(t *testing.T) {
	var payload map[string]json.RawMessage
	json.Unmarshal([]byte(`{"model":"ag/gemini-3.8-flash-high","request":{"contents":[{"role":"user","parts":[{"text":"test"}]}],"tools":[{"functionDeclarations":[{"name":"pwd"}]}],"toolConfig":{"functionCallingConfig":{"mode":"VALIDATED"}}}}`), &payload)
	encoded, err := nativeToOpenAI(payload)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	json.Unmarshal(encoded, &result)
	if result["tool_choice"] != "auto" {
		t.Fatal("validated mode not converted")
	}
}
