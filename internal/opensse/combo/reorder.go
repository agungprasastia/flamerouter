package combo

import (
	"encoding/json"
	"flamerouter/internal/provider"
	"strings"
)

// hard caps: missing one drops request data.
var hardCaps = map[string]bool{
	"vision": true, "pdf": true, "audioInput": true, "videoInput": true,
}

func scanBlockType(b map[string]any, required map[string]bool) {
	t, ok := b["type"].(string)
	if !ok {
		return
	}

	switch t {
	case "image_url", "image", "input_image":
		required["vision"] = true
	case "file", "document", "input_file":
		required["pdf"] = true
	case "input_audio", "audio":
		required["audioInput"] = true
	}
}

func scanInlineOrFileData(b map[string]any, key string, required map[string]bool) {
	data, ok := b[key].(map[string]any)
	if !ok {
		return
	}

	mime, ok := data["mimeType"].(string)
	if !ok {
		return
	}

	if strings.HasPrefix(mime, "image/") {
		required["vision"] = true
	} else if mime == "application/pdf" {
		required["pdf"] = true
	}
}

func scanBlock(b map[string]any, required map[string]bool) {
	if b == nil {
		return
	}

	scanBlockType(b, required)
	scanInlineOrFileData(b, "inlineData", required)
	scanInlineOrFileData(b, "fileData", required)
}

func scanContent(content any, required map[string]bool) {
	arr, ok := content.([]any)
	if !ok {
		return
	}

	for _, b := range arr {
		if m, ok := b.(map[string]any); ok {
			scanBlock(m, required)
		}
	}
}

func extractContentsField(body map[string]any) any {
	if c, ok := body["contents"]; ok {
		return c
	}

	if req, ok := body["request"].(map[string]any); ok {
		return req["contents"]
	}

	return nil
}

// DetectRequiredCapabilities scans body for modalities on the current user turn.
func DetectRequiredCapabilities(body map[string]any) map[string]bool {
	required := map[string]bool{}
	if body == nil {
		return required
	}

	for _, m := range trailingUserItems(body["messages"]) {
		if mm, ok := m.(map[string]any); ok {
			scanContent(mm["content"], required)
		}
	}

	for _, m := range trailingUserItems(body["input"]) {
		if mm, ok := m.(map[string]any); ok {
			scanContent(mm["content"], required)
		}
	}

	for _, m := range trailingUserItems(extractContentsField(body)) {
		if mm, ok := m.(map[string]any); ok {
			scanContent(mm["parts"], required)
		}
	}

	return required
}

func trailingUserItems(arr any) []any {
	items, ok := arr.([]any)
	if !ok || len(items) == 0 {
		return nil
	}

	isAssistant := func(r string) bool { return r == "assistant" || r == "model" }

	i := len(items) - 1
	for i >= 0 {
		m, ok := items[i].(map[string]any)
		if !ok {
			break
		}

		role, ok := m["role"].(string)
		if ok && isAssistant(role) {
			break
		}

		i--
	}

	return items[i+1:]
}

// ReorderForCapabilities promotes models that support the request's modalities.
func ReorderForCapabilities(models []string, body []byte) []string {
	if len(models) <= 1 {
		return models
	}

	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return models
	}

	required := DetectRequiredCapabilities(m)
	if len(required) == 0 {
		return models
	}

	return reorderByCapabilities(models, required)
}

func reorderByCapabilities(models []string, required map[string]bool) []string {
	var hard, soft []string

	for c := range required {
		if hardCaps[c] {
			hard = append(hard, c)
		} else {
			soft = append(soft, c)
		}
	}

	type item struct {
		m string
		i int
		t int
	}

	items := make([]item, len(models))
	for i, modelStr := range models {
		items[i] = item{m: modelStr, i: i, t: tierOf(modelStr, hard, soft)}
	}
	// stable sort by tier
	for a := 0; a < len(items); a++ {
		for b := a + 1; b < len(items); b++ {
			if items[b].t < items[a].t || (items[b].t == items[a].t && items[b].i < items[a].i) {
				items[a], items[b] = items[b], items[a]
			}
		}
	}

	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.m
	}

	return out
}

func modelHasCapability(caps provider.Capabilities, name string) bool {
	switch name {
	case "vision":
		return caps.Vision
	case "pdf":
		return caps.PDF
	case "audioInput":
		return caps.AudioInput
	case "videoInput":
		return caps.VideoInput
	case "search":
		return caps.Search
	default:
		return false
	}
}

func tierOf(modelStr string, hard, soft []string) int {
	modelName := splitProviderModel(modelStr)
	caps := provider.GetCapabilities(modelName)

	for _, c := range hard {
		if !modelHasCapability(caps, c) {
			return 2
		}
	}

	for _, c := range soft {
		if !modelHasCapability(caps, c) {
			return 1
		}
	}

	return 0
}

func splitProviderModel(s string) string {
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i+1:]
	}

	return s
}
