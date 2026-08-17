package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/schema"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatCommandCode, openaiToCommandCodeRequest, nil)
}

func openaiToCommandCodeRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	messages, system := convertCommandCodeMessages(body)
	params := map[string]any{
		"model":       model,
		"messages":    messages,
		"stream":      stream,
		"max_tokens":  getMaxTokens(body),
		"temperature": getTemperature(body),
	}

	if system != "" {
		params["system"] = system
	}

	tools := convertCommandCodeTools(body)
	if tools != nil {
		params["tools"] = tools
	}

	if tp, ok := body["top_p"]; ok {
		params["top_p"] = tp
	}

	today := time.Now().Format("2006-01-02")

	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}

	return map[string]any{
		"threadId": uuid.New().String(),
		"memory":   "",
		"config": map[string]any{
			"workingDir":    wd,
			"date":          today,
			"environment":   runtime.GOOS,
			"structure":     []any{},
			"isGitRepo":     false,
			"currentBranch": "",
			"mainBranch":    "",
			"gitStatus":     "",
			"recentCommits": []any{},
		},
		"params": params,
	}
}

func convertCCToolResultMsg(msg map[string]any) map[string]any {
	content := msg["content"]

	var value string
	if s, ok := content.(string); ok {
		value = s
	} else {
		value = flattenCommandCodeContent(content)
	}

	return map[string]any{
		"role": schema.RoleTool,
		"content": []any{map[string]any{
			"type":       "tool-result",
			"toolCallId": msg["tool_call_id"],
			"toolName":   msg["name"],
			"output":     map[string]any{"type": "text", "value": value},
		}},
	}
}

func parseSingleCCToolCallBlock(tcRaw any) (map[string]any, bool) {
	toolCall, ok := tcRaw.(map[string]any)
	if !ok || toolCall == nil {
		return nil, false
	}

	name := ""
	argsStr := ""

	if fn, fnOk := toolCall["function"].(map[string]any); fnOk && fn != nil {
		if n, nOk := fn["name"].(string); nOk {
			name = n
		}

		if a, aOk := fn["arguments"].(string); aOk {
			argsStr = a
		}
	}

	var args map[string]any
	if argsStr != "" {
		safeParseCommandCodeJSON(argsStr, &args)
	}

	return map[string]any{
		"type":       "tool-call",
		"toolCallId": toolCall["id"],
		"toolName":   name,
		"input":      args,
	}, true
}

func convertCCAssistantBlocks(msg map[string]any) []any {
	var blocks []any

	text := flattenCommandCodeContent(msg["content"])
	if text != "" {
		blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": text})
	}

	if tc, ok := msg["tool_calls"].([]any); ok {
		for _, tcRaw := range tc {
			if block, ok := parseSingleCCToolCallBlock(tcRaw); ok {
				blocks = append(blocks, block)
			}
		}
	}

	if len(blocks) == 0 {
		blocks = []any{map[string]any{"type": schema.OpenaiBlockText, "text": ""}}
	}

	return blocks
}

func convertCommandCodeMessages(body map[string]any) ([]any, string) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return nil, ""
	}

	out := make([]any, 0, len(messages))

	var systemTexts []string

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, roleOk := msg["role"].(string)
		if !roleOk {
			role = ""
		}

		switch role {
		case schema.RoleSystem:
			text := flattenCommandCodeContent(msg["content"])
			if text != "" {
				systemTexts = append(systemTexts, text)
			}
		case schema.RoleTool:
			out = append(out, convertCCToolResultMsg(msg))
		case schema.RoleAssistant:
			out = append(out, map[string]any{"role": schema.RoleAssistant, "content": convertCCAssistantBlocks(msg)})
		default:
			out = append(out, map[string]any{
				"role":    schema.RoleUser,
				"content": toCommandCodeContentBlocks(msg["content"]),
			})
		}
	}

	return out, strings.Join(systemTexts, "\n\n")
}

func parseCCFunctionTool(tool map[string]any) map[string]any {
	fn, ok := tool["function"].(map[string]any)
	if !ok || fn == nil {
		return nil
	}

	name, ok := fn["name"].(string)
	if !ok {
		name = ""
	}

	desc, ok := fn["description"].(string)
	if !ok {
		desc = ""
	}

	params, ok := fn["parameters"].(map[string]any)
	if !ok || params == nil {
		params = map[string]any{"type": "object"}
	}

	return map[string]any{
		"name":         name,
		"description":  desc,
		"input_schema": params,
	}
}

func parseCCNamedTool(tool map[string]any) map[string]any {
	name, ok := tool["name"].(string)
	if !ok || name == "" {
		return nil
	}

	desc, ok := tool["description"].(string)
	if !ok {
		desc = ""
	}

	params, ok := tool["input_schema"].(map[string]any)
	if !ok || params == nil {
		if p, pOk := tool["parameters"].(map[string]any); pOk && p != nil {
			params = p
		}
	}

	if params == nil {
		params = map[string]any{"type": "object"}
	}

	return map[string]any{
		"name":         name,
		"description":  desc,
		"input_schema": params,
	}
}

func parseCCToolDef(tool map[string]any) map[string]any {
	if tool["type"] == schema.OpenaiBlockFunction {
		return parseCCFunctionTool(tool)
	}

	return parseCCNamedTool(tool)
}

func convertCommandCodeTools(body map[string]any) []any {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return nil
	}

	result := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			continue
		}

		if parsed := parseCCToolDef(tool); parsed != nil {
			result = append(result, parsed)
		}
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func flattenCommandCodeContent(content any) string {
	if content == nil {
		return ""
	}

	if s, ok := content.(string); ok {
		return s
	}

	if arr, ok := content.([]any); ok {
		var parts []string

		for _, p := range arr {
			switch v := p.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if text, ok := v["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}

		return strings.Join(parts, "\n")
	}

	return ""
}

func parseCCContentBlock(p any) any {
	switch v := p.(type) {
	case string:
		return map[string]any{"type": schema.OpenaiBlockText, "text": v}
	case map[string]any:
		btype, ok := v["type"].(string)
		if ok && (btype == schema.OpenaiBlockImageURL || btype == "image") {
			return map[string]any{"type": schema.OpenaiBlockText, "text": "[image omitted]"}
		}

		if text, ok := v["text"].(string); ok {
			return map[string]any{"type": schema.OpenaiBlockText, "text": text}
		}
	}

	return nil
}

func toCommandCodeContentBlocks(content any) []any {
	if content == nil {
		return []any{map[string]any{"type": schema.OpenaiBlockText, "text": ""}}
	}

	if s, ok := content.(string); ok {
		return []any{map[string]any{"type": schema.OpenaiBlockText, "text": s}}
	}

	if arr, ok := content.([]any); ok {
		var blocks []any

		for _, p := range arr {
			if b := parseCCContentBlock(p); b != nil {
				blocks = append(blocks, b)
			}
		}

		if len(blocks) == 0 {
			blocks = []any{map[string]any{"type": schema.OpenaiBlockText, "text": ""}}
		}

		return blocks
	}

	return []any{map[string]any{"type": schema.OpenaiBlockText, "text": ""}}
}

func getMaxTokens(body map[string]any) int {
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		return int(mt)
	}

	if mot, ok := body["max_output_tokens"].(float64); ok && mot > 0 {
		return int(mot)
	}

	return 8192
}

func getTemperature(body map[string]any) float64 {
	if temp, ok := body["temperature"].(float64); ok {
		return temp
	}

	return 0.3
}

func safeParseCommandCodeJSON(s string, target any) {
	if s == "" {
		return
	}

	if err := json.Unmarshal([]byte(s), target); err != nil {
		return
	}
}
