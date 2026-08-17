package concerns

import (
	"fmt"
	"strings"
)

var placeholderCurrent = map[string]string{
	"vision":     "[image omitted: model has no vision support]",
	"audioInput": "[audio omitted: model has no audio support]",
	"pdf":        "[file omitted: model has no document support]",
}

var placeholderPrev = map[string]string{
	"vision":     "[Previous image omitted from context.]",
	"audioInput": "[Previous audio omitted from context.]",
	"pdf":        "[Previous file omitted from context.]",
}

func ph(cap string, isLast bool) string {
	if isLast {
		return placeholderCurrent[cap]
	}

	return placeholderPrev[cap]
}

func capForMime(mime string) string {
	if strings.HasPrefix(mime, "image/") {
		return "vision"
	}

	if strings.HasPrefix(mime, "audio/") {
		return "audioInput"
	}

	if mime == "application/pdf" {
		return "pdf"
	}

	return ""
}

func capForOpenAIBlock(block map[string]any) string {
	t, ok := block["type"].(string)
	if !ok {
		return ""
	}

	switch t {
	case "image_url", "image":
		return "vision"
	case "input_audio", "audio_url":
		return "audioInput"
	case "file":
		return "pdf"
	}

	return ""
}

func capForClaudeBlock(block map[string]any) string {
	t, ok := block["type"].(string)
	if !ok {
		return ""
	}

	switch t {
	case "image":
		return "vision"
	case "document":
		return "pdf"
	}

	return ""
}

func filterBlocks(blocks []any, capOf func(map[string]any) string, caps *Capabilities, isLast bool) []any {
	removed := map[string]bool{}
	out := make([]any, 0, len(blocks))

	for _, block := range blocks {
		b, ok := block.(map[string]any)
		if !ok {
			out = append(out, block)
			continue
		}

		cap := capOf(b)
		if cap != "" && !capEnabled(caps, cap) {
			removed[cap] = true
			continue
		}

		out = append(out, block)
	}

	for cap := range removed {
		out = append(out, map[string]any{"type": "text", "text": ph(cap, isLast)})
	}

	return out
}

func capEnabled(caps *Capabilities, cap string) bool {
	if caps == nil {
		return true
	}

	switch cap {
	case "vision":
		return caps.Vision
	case "audioInput":
		return caps.AudioInput
	case "pdf":
		return caps.PDF
	}

	return true
}

func stripOpenAI(body map[string]any, caps *Capabilities) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	last := len(messages) - 1

	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		msg["content"] = filterBlocks(content, capForOpenAIBlock, caps, i == last)
	}
}

func stripClaude(body map[string]any, caps *Capabilities) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	last := len(messages) - 1

	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		msg["content"] = filterBlocks(content, capForClaudeBlock, caps, i == last)
	}
}

func stripResponses(body map[string]any, caps *Capabilities) {
	input, ok := body["input"].([]any)
	if !ok {
		return
	}

	last := len(input) - 1

	for i, itemRaw := range input {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		content, ok := item["content"].([]any)
		if !ok {
			continue
		}

		item["content"] = filterResponseBlocks(content, caps, i == last)
	}
}

func filterResponseBlocks(content []any, caps *Capabilities, isLast bool) []any {
	removed := map[string]bool{}
	out := make([]any, 0, len(content))

	for _, b := range content {
		block, ok := b.(map[string]any)
		if !ok {
			out = append(out, b)
			continue
		}

		t, ok := block["type"].(string)
		if !ok {
			out = append(out, b)
			continue
		}

		cap := ""
		if t == "input_image" {
			cap = "vision"
		} else if t == "input_file" {
			cap = "pdf"
		}

		if cap != "" && !capEnabled(caps, cap) {
			removed[cap] = true
			continue
		}

		out = append(out, block)
	}

	for cap := range removed {
		out = append(out, map[string]any{"type": "input_text", "text": ph(cap, isLast)})
	}

	return out
}

func extractGeminiMime(p map[string]any) string {
	mime := ""

	if id, ok := p["inlineData"].(map[string]any); ok {
		if m, ok := id["mimeType"].(string); ok {
			mime = m
		} else if m, ok := id["mime_type"].(string); ok {
			mime = m
		}
	}

	if fd, ok := p["fileData"].(map[string]any); ok {
		if m, ok := fd["mimeType"].(string); ok {
			mime = m
		}
	}

	return mime
}

func stripGeminiContentParts(parts []any, caps *Capabilities, isLast bool) []any {
	removed := map[string]bool{}
	out := make([]any, 0, len(parts))

	for _, pRaw := range parts {
		p, ok := pRaw.(map[string]any)
		if !ok {
			out = append(out, pRaw)
			continue
		}

		mime := extractGeminiMime(p)

		cap := capForMime(mime)
		if cap != "" && !capEnabled(caps, cap) {
			removed[cap] = true
			continue
		}

		out = append(out, p)
	}

	for cap := range removed {
		out = append(out, map[string]any{"text": ph(cap, isLast)})
	}

	return out
}

func stripGeminiParts(contents []any, caps *Capabilities) {
	if contents == nil {
		return
	}

	last := len(contents) - 1

	for i, cRaw := range contents {
		c, ok := cRaw.(map[string]any)
		if !ok {
			continue
		}

		parts, ok := c["parts"].([]any)
		if !ok {
			continue
		}

		c["parts"] = stripGeminiContentParts(parts, caps, i == last)
	}
}

func stripGeminiModalities(body map[string]any, caps *Capabilities) {
	if contents, ok := body["contents"].([]any); ok {
		stripGeminiParts(contents, caps)
	}
}

func stripAntigravityModalities(body map[string]any, caps *Capabilities) {
	if req, ok := body["request"].(map[string]any); ok {
		if contents, ok := req["contents"].([]any); ok {
			stripGeminiParts(contents, caps)
		}
	}
}

// StripUnsupportedModalities removes media blocks model can't read.
// sourceFormat: openai|claude|openai-responses|gemini|antigravity|...
func StripUnsupportedModalities(body map[string]any, sourceFormat string, caps *Capabilities) bool {
	if body == nil || caps == nil {
		return false
	}

	if caps.Vision && caps.AudioInput && caps.PDF {
		return false
	}

	dispatchStripModalities(body, sourceFormat, caps)

	return true
}

func dispatchStripModalities(body map[string]any, sourceFormat string, caps *Capabilities) {
	switch sourceFormat {
	case "openai", "ollama", "kiro", "cursor", "commandcode":
		stripOpenAI(body, caps)
	case "claude":
		stripClaude(body, caps)
	case "openai-responses", "openai-response", "codex", "responses":
		stripResponses(body, caps)
	case "gemini", "gemini-cli", "vertex":
		stripGeminiModalities(body, caps)
	case "antigravity":
		stripAntigravityModalities(body, caps)
	default:
		stripOpenAI(body, caps)
	}
}

// StripUnsupportedModalitiesLegacy keeps old signature used by some callers.
func StripUnsupportedModalitiesLegacy(body map[string]any, caps Capabilities, _ /* isCurrentTurn */ bool) map[string]any {
	c := caps
	StripUnsupportedModalities(body, "openai", &c)

	return body
}

// CapabilitiesFromServiceKind derives default Capabilities for a given service kind.
func CapabilitiesFromServiceKind(kind string) *Capabilities {
	base := defaultCaps()

	switch kind {
	case "imageToText":
		base.Vision = true
		return base
	case "image":
		base.ImageOutput = true
		return base
	case "stt":
		base.AudioInput = true
		return base
	case "tts":
		base.AudioOutput = true
		return base
	case "embedding":
		base.Tools = false
		return base
	}

	return nil
}

type stripRule struct {
	provider              string
	match                 func(string) bool
	drop                  []string
	flattenContent        bool
	clampToModelMaxOutput bool
	maxOutputCap          int
}

var stripRules = []stripRule{
	{
		provider:              "",
		match:                 func(m string) bool { return strings.Contains(m, "claude") },
		drop:                  []string{"temperature"},
		flattenContent:        false,
		clampToModelMaxOutput: false,
		maxOutputCap:          0,
	},
	{
		provider:              "github",
		match:                 func(m string) bool { return strings.Contains(m, "gpt-5.4") },
		drop:                  []string{"temperature"},
		flattenContent:        false,
		clampToModelMaxOutput: false,
		maxOutputCap:          0,
	},
	{
		provider: "github",
		match: func(m string) bool {
			return strings.Contains(m, "claude") && !strings.Contains(m, "opus") && !strings.Contains(m, "sonnet") && !strings.Contains(m, "4.6")
		},
		drop:                  []string{"thinking", "reasoning_effort"},
		flattenContent:        false,
		clampToModelMaxOutput: false,
		maxOutputCap:          0,
	},
	{
		provider:              "cloudflare-ai",
		match:                 nil,
		drop:                  nil,
		flattenContent:        true,
		clampToModelMaxOutput: false,
		maxOutputCap:          0,
	},
	{
		provider:              "volcengine-ark",
		match:                 func(m string) bool { return strings.Contains(m, "glm-5") },
		drop:                  nil,
		flattenContent:        false,
		clampToModelMaxOutput: true,
		maxOutputCap:          0,
	},
	{
		provider:              "volcengine-ark",
		match:                 func(m string) bool { return strings.Contains(m, "kimi") },
		drop:                  nil,
		flattenContent:        false,
		clampToModelMaxOutput: true,
		maxOutputCap:          32768,
	},
}

func matchesRule(rule stripRule, model string) bool {
	if rule.match == nil {
		return true
	}

	return rule.match(model)
}

func clampNumber(body map[string]any, key string, ceiling int) {
	if v, ok := body[key].(float64); ok && int(v) > ceiling {
		body[key] = float64(ceiling)
	}

	if v, ok := body[key].(int); ok && v > ceiling {
		body[key] = ceiling
	}
}

func flattenMessagesContent(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		var parts []string

		for _, b := range contentArr {
			block, ok := b.(map[string]any)
			if !ok {
				continue
			}

			if t, ok := block["type"].(string); ok && t == "text" {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}

		msg["content"] = strings.Join(parts, "")
	}
}

func applyOutputClamping(provider, model string, body map[string]any, rule stripRule) {
	if !rule.clampToModelMaxOutput && rule.maxOutputCap <= 0 {
		return
	}

	caps := GetCapabilitiesForModel(provider, model)

	var candidates []int

	if rule.clampToModelMaxOutput && caps.MaxOutput > 0 {
		candidates = append(candidates, caps.MaxOutput)
	}

	if rule.maxOutputCap > 0 {
		candidates = append(candidates, rule.maxOutputCap)
	}

	if len(candidates) == 0 {
		return
	}

	ceiling := candidates[0]
	for _, c := range candidates[1:] {
		if c < ceiling {
			ceiling = c
		}
	}

	clampNumber(body, "max_tokens", ceiling)
	clampNumber(body, "max_completion_tokens", ceiling)
	clampNumber(body, "max_output_tokens", ceiling)
}

// StripUnsupportedParams cleans up provider-incompatible parameters in request body.
func StripUnsupportedParams(provider, model string, body map[string]any) map[string]any {
	if body == nil || model == "" {
		return body
	}

	for _, rule := range stripRules {
		if rule.provider != "" && rule.provider != provider {
			continue
		}

		if !matchesRule(rule, model) {
			continue
		}

		for _, key := range rule.drop {
			delete(body, key)
		}

		if rule.flattenContent {
			flattenMessagesContent(body)
		}

		applyOutputClamping(provider, model, body, rule)
	}

	return body
}

// ClampMaxTokens limits max_tokens/max_completion_tokens in body to maxOutput.
func ClampMaxTokens(_ /* model */, body map[string]any, maxOutput int) map[string]any {
	if body == nil || maxOutput <= 0 {
		return body
	}

	if mt, ok := body["max_tokens"].(float64); ok {
		if int(mt) > maxOutput {
			body["max_tokens"] = float64(maxOutput)
		}
	}

	if mt, ok := body["max_completion_tokens"].(float64); ok {
		if int(mt) > maxOutput {
			body["max_completion_tokens"] = float64(maxOutput)
		}
	}

	return body
}

// FormatProviderError formats standard provider error string.
func FormatProviderError(err error, provider, model string, status int) string {
	if err == nil {
		return ""
	}

	return fmt.Sprintf("%s/%s: %s (HTTP %d)", provider, model, err.Error(), status)
}
