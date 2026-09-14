package routerbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNonstreamCombinesTextAndToolCalls(t *testing.T) {
	data := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}}

data: {"response":{"candidates":[{"content":{"parts":[{"functionCall":{"name":"pwd","args":{}}}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":12}}}

data: [DONE]
`
	encoded, err := collectGeneration(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Response struct {
			Candidates []struct {
				Content      struct{ Parts []map[string]any }
				FinishReason string
			}
			UsageMetadata struct{ TotalTokenCount int }
		}
	}
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Response.Candidates) != 1 || len(output.Response.Candidates[0].Content.Parts) != 2 || output.Response.Candidates[0].Content.Parts[1]["functionCall"] == nil || output.Response.Candidates[0].FinishReason != "STOP" || output.Response.UsageMetadata.TotalTokenCount != 12 {
		t.Fatal("generation was damaged")
	}
}

func TestNonstreamRejectsErrorAndTruncatedFrames(t *testing.T) {
	for _, input := range []string{`data: {"error":{"message":"failed"}}`, `data: {"response":`, ""} {
		if _, err := collectGeneration(strings.NewReader(input)); err == nil {
			t.Fatal("invalid stream accepted")
		}
	}
}
