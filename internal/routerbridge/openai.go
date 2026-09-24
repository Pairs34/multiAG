package routerbridge

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

func nativeToOpenAI(payload map[string]json.RawMessage) ([]byte, error) {
	var body struct {
		Contents []struct {
			Role  string
			Parts []map[string]any
		}
		SystemInstruction struct{ Parts []struct{ Text string } }
		Tools             []struct{ FunctionDeclarations []map[string]any }
		GenerationConfig  map[string]any
		ToolConfig        struct {
			FunctionCallingConfig struct {
				Mode                 string
				AllowedFunctionNames []string
			}
		}
	}
	if err := json.Unmarshal(payload["request"], &body); err != nil {
		return nil, errors.New("invalid native request")
	}
	var model string
	json.Unmarshal(payload["model"], &model)
	messages := []map[string]any{}
	system := []string{}
	for _, p := range body.SystemInstruction.Parts {
		system = append(system, p.Text)
	}
	if len(system) > 0 {
		messages = append(messages, map[string]any{"role": "system", "content": strings.Join(system, "\n")})
	}
	pending := map[string][]string{}
	seq := 0
	for _, content := range body.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		if role != "assistant" && role != "user" {
			return nil, errors.New("unsupported content role")
		}
		parts := []any{}
		calls := []any{}
		results := []map[string]any{}
		reasoning := ""
		for _, part := range content.Parts {
			if text, ok := part["text"].(string); ok && text != "" {
				if part["thought"] == true {
					reasoning += text
				} else {
					parts = append(parts, map[string]any{"type": "text", "text": text})
				}
			}
			image, hasImage := part["inlineData"].(map[string]any)
			if !hasImage {
				image, hasImage = part["inline_data"].(map[string]any)
			}
			if hasImage {
				mime, _ := image["mimeType"].(string)
				if mime == "" {
					mime, _ = image["mime_type"].(string)
				}
				data, _ := image["data"].(string)
				if !strings.HasPrefix(mime, "image/") {
					return nil, errors.New("unsupported inline media")
				}
				if _, err := base64.StdEncoding.DecodeString(data); err != nil {
					return nil, errors.New("invalid image data")
				}
				parts = append(parts, map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:" + mime + ";base64," + data}})
			}
			if _, ok := part["fileData"]; ok {
				return nil, errors.New("file URI media is not supported in compatibility mode")
			}
			if call, ok := part["functionCall"].(map[string]any); ok {
				name, _ := call["name"].(string)
				if name == "" {
					return nil, errors.New("missing tool name")
				}
				seq++
				id, _ := call["id"].(string)
				if id == "" {
					id = "ag_call_" + jsonNumber(seq)
				}
				pending[name] = append(pending[name], id)
				args := call["args"]
				if args == nil {
					args = map[string]any{}
				}
				encoded, _ := json.Marshal(args)
				calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}})
			}
			if result, ok := part["functionResponse"].(map[string]any); ok {
				name, _ := result["name"].(string)
				id, _ := result["id"].(string)
				if len(pending[name]) > 0 {
					if id == "" {
						id = pending[name][0]
						pending[name] = pending[name][1:]
					} else {
						for i, waiting := range pending[name] {
							if waiting == id {
								pending[name] = append(pending[name][:i], pending[name][i+1:]...)
								break
							}
						}
					}
				}
				if id == "" {
					return nil, errors.New("tool response has no matching call")
				}
				encoded, _ := json.Marshal(result["response"])
				results = append(results, map[string]any{"role": "tool", "tool_call_id": id, "content": string(encoded)})
			}
		}
		if len(parts) > 0 || len(calls) > 0 || reasoning != "" {
			message := map[string]any{"role": role, "content": nil}
			if len(parts) > 0 {
				message["content"] = parts
			}
			if len(calls) > 0 {
				message["tool_calls"] = calls
			}
			if reasoning != "" {
				message["reasoning_content"] = reasoning
			}
			messages = append(messages, message)
		}
		messages = append(messages, results...)
	}
	if len(messages) == 0 {
		return nil, errors.New("empty generation request")
	}
	output := map[string]any{"model": model, "messages": messages, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	for native, openai := range map[string]string{"maxOutputTokens": "max_tokens", "temperature": "temperature", "topP": "top_p"} {
		if value, ok := body.GenerationConfig[native]; ok {
			output[openai] = value
		}
	}
	tools := []any{}
	for _, tool := range body.Tools {
		if len(tool.FunctionDeclarations) == 0 {
			return nil, errors.New("unsupported native tool in compatibility mode")
		}
		for _, fn := range tool.FunctionDeclarations {
			parameters := fn["parameters"]
			if parameters == nil {
				parameters = fn["parametersJsonSchema"]
			}
			if parameters == nil {
				parameters = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			function := map[string]any{"name": fn["name"], "parameters": normalizeToolSchema(parameters)}
			if description, ok := fn["description"]; ok {
				function["description"] = description
			}
			tools = append(tools, map[string]any{"type": "function", "function": function})
		}
	}
	if len(tools) > 0 {
		output["tools"] = tools
		switch config := body.ToolConfig.FunctionCallingConfig; config.Mode {
		case "", "AUTO", "MODE_UNSPECIFIED", "VALIDATED":
			output["tool_choice"] = "auto"
		case "NONE":
			output["tool_choice"] = "none"
		case "ANY":
			if len(config.AllowedFunctionNames) == 1 {
				output["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": config.AllowedFunctionNames[0]}}
			} else if len(config.AllowedFunctionNames) == 0 {
				output["tool_choice"] = "required"
			} else {
				return nil, errors.New("restricted tool list unsupported in compatibility mode")
			}
		default:
			return nil, errors.New("unsupported tool calling mode")
		}
	}
	return json.Marshal(output)
}

func jsonNumber(n int) string { b, _ := json.Marshal(n); return string(b) }
func normalizeToolSchema(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, val := range v {
			if key == "enumDescriptions" {
				continue
			}
			if key == "type" {
				if s, ok := val.(string); ok {
					val = strings.ToLower(s)
				}
			}
			out[key] = normalizeToolSchema(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeToolSchema(item)
		}
		return out
	default:
		return value
	}
}

func openAIToNative(reader io.Reader, emit func([]byte) error) error {
	return openAIToNativeMode(reader, emit, true)
}

func openAIToNativeStream(reader io.Reader, emit func([]byte) error) error {
	return openAIToNativeMode(reader, emit, false)
}

func openAIToNativeMode(reader io.Reader, emit func([]byte) error, emitUsageOnly bool) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	type toolCall struct{ Name, Arguments string }
	type candidateState struct {
		Calls    map[int]*toolCall
		Finished bool
	}
	states := map[int]*candidateState{}
	responseID, model := "", ""
	checkEnd := func() error {
		if len(states) == 0 {
			return errors.New("empty router stream")
		}
		for _, state := range states {
			if len(state.Calls) > 0 || !state.Finished {
				return errors.New("unfinished router stream")
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return checkEnd()
		}
		var chunk struct {
			ID, Model string
			Error     json.RawMessage
			Choices   []struct {
				Index        int
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Content   string
					Reasoning string `json:"reasoning_content"`
					ToolCalls []struct {
						Index    int
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				}
			}
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return err
		}
		if chunk.Error != nil {
			return errors.New("router stream error")
		}
		if chunk.ID != "" {
			responseID = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		response := map[string]any{"responseId": responseID, "modelVersion": model}
		candidates := []any{}
		for _, choice := range chunk.Choices {
			state := states[choice.Index]
			if state == nil {
				state = &candidateState{Calls: map[int]*toolCall{}}
				states[choice.Index] = state
			}
			parts := []any{}
			if choice.Delta.Reasoning != "" {
				parts = append(parts, map[string]any{"thought": true, "text": choice.Delta.Reasoning})
			}
			if choice.Delta.Content != "" {
				parts = append(parts, map[string]any{"text": choice.Delta.Content})
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := state.Calls[delta.Index]
				if call == nil {
					call = &toolCall{}
					state.Calls[delta.Index] = call
				}
				call.Name += delta.Function.Name
				call.Arguments += delta.Function.Arguments
			}
			candidate := map[string]any{"index": choice.Index}
			if choice.FinishReason != nil {
				indices := []int{}
				for index := range state.Calls {
					indices = append(indices, index)
				}
				sort.Ints(indices)
				for _, i := range indices {
					call := state.Calls[i]
					if call == nil {
						return errors.New("invalid tool call index")
					}
					var args map[string]any
					if call.Arguments == "" {
						args = map[string]any{}
					} else if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
						return errors.New("incomplete tool arguments")
					}
					if call.Name == "" || args == nil {
						return errors.New("invalid tool call")
					}
					parts = append(parts, map[string]any{"functionCall": map[string]any{"name": call.Name, "args": args}})
				}
				state.Calls = map[int]*toolCall{}
				state.Finished = true
				finish := "STOP"
				if *choice.FinishReason == "length" {
					finish = "MAX_TOKENS"
				}
				if *choice.FinishReason == "content_filter" {
					finish = "SAFETY"
				}
				candidate["finishReason"] = finish
				if len(parts) == 0 {
					parts = append(parts, map[string]any{"text": ""})
				}
			}
			if len(parts) > 0 {
				candidate["content"] = map[string]any{"role": "model", "parts": parts}
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) > 0 {
			response["candidates"] = candidates
		}
		if chunk.Usage != nil {
			response["usageMetadata"] = map[string]any{"promptTokenCount": chunk.Usage.PromptTokens, "candidatesTokenCount": chunk.Usage.CompletionTokens, "totalTokenCount": chunk.Usage.TotalTokens}
		}
		// Some routers send usage in a separate, candidate-less event. The IDE
		// stream consumer assumes a response event has a candidate and can crash
		// while dereferencing a usage-only frame. Router-side accounting retains
		// this data, so streaming callers suppress that frame. Non-stream callers
		// keep it so collectGeneration can merge usage into the final response.
		if len(candidates) > 0 || (emitUsageOnly && chunk.Usage != nil) {
			encoded, _ := json.Marshal(map[string]any{"response": response})
			if err := emit(encoded); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return checkEnd()
}
