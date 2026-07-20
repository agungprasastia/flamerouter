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

func openaiToCommandCodeRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	messages, system := convertCommandCodeMessages(body)
	params := map[string]any{
		"model":      model,
		"messages":   messages,
		"stream":     stream,
		"max_tokens": getMaxTokens(body),
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
	wd, _ := os.Getwd()

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

func convertCommandCodeMessages(body map[string]any) ([]any, string) {
	messages, _ := body["messages"].([]any)
	var out []any
	var systemTexts []string

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		if role == schema.RoleSystem {
			text := flattenCommandCodeContent(msg["content"])
			if text != "" {
				systemTexts = append(systemTexts, text)
			}
			continue
		}

		if role == schema.RoleTool {
			content := msg["content"]
			var value string
			if s, ok := content.(string); ok {
				value = s
			} else {
				value = flattenCommandCodeContent(content)
			}
			out = append(out, map[string]any{
				"role": schema.RoleTool,
				"content": []any{map[string]any{
					"type":       "tool-result",
					"toolCallId": msg["tool_call_id"],
					"toolName":   msg["name"],
					"output":     map[string]any{"type": "text", "value": value},
				}},
			})
			continue
		}

		if role == schema.RoleAssistant {
			var blocks []any
			text := flattenCommandCodeContent(msg["content"])
			if text != "" {
				blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": text})
			}
			if tc, ok := msg["tool_calls"].([]any); ok {
				for _, tcRaw := range tc {
					toolCall, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}
					fn, _ := toolCall["function"].(map[string]any)
					name, _ := fn["name"].(string)
					argsStr, _ := fn["arguments"].(string)
					var args map[string]any
					if argsStr != "" {
						safeParseCommandCodeJSON(argsStr, &args)
					}
					blocks = append(blocks, map[string]any{
						"type":       "tool-call",
						"toolCallId": toolCall["id"],
						"toolName":   name,
						"input":      args,
					})
				}
			}
			if len(blocks) == 0 {
				blocks = []any{map[string]any{"type": schema.OpenaiBlockText, "text": ""}}
			}
			out = append(out, map[string]any{"role": schema.RoleAssistant, "content": blocks})
			continue
		}

		out = append(out, map[string]any{
			"role":    schema.RoleUser,
			"content": toCommandCodeContentBlocks(msg["content"]),
		})
	}

	return out, strings.Join(systemTexts, "\n\n")
}

func convertCommandCodeTools(body map[string]any) []any {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return nil
	}
	var result []any
	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			continue
		}
		if tool["type"] == schema.OpenaiBlockFunction {
			fn, ok := tool["function"].(map[string]any)
			if !ok {
				continue
			}
			name, _ := fn["name"].(string)
			desc, _ := fn["description"].(string)
			params, _ := fn["parameters"].(map[string]any)
			if params == nil {
				params = map[string]any{"type": "object"}
			}
			result = append(result, map[string]any{
				"name":         name,
				"description":  desc,
				"input_schema": params,
			})
		} else if name, ok := tool["name"].(string); ok && name != "" {
			desc, _ := tool["description"].(string)
			params, _ := tool["input_schema"].(map[string]any)
			if params == nil {
				params, _ = tool["parameters"].(map[string]any)
			}
			if params == nil {
				params = map[string]any{"type": "object"}
			}
			result = append(result, map[string]any{
				"name":         name,
				"description":  desc,
				"input_schema": params,
			})
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
			switch v := p.(type) {
			case string:
				blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": v})
			case map[string]any:
				btype, _ := v["type"].(string)
				switch btype {
				case schema.OpenaiBlockText:
					if text, ok := v["text"].(string); ok {
						blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": text})
					}
				case schema.OpenaiBlockImageUrl, "image":
					blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": "[image omitted]"})
				default:
					if text, ok := v["text"].(string); ok {
						blocks = append(blocks, map[string]any{"type": schema.OpenaiBlockText, "text": text})
					}
				}
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
	_ = json.Unmarshal([]byte(s), target)
}
