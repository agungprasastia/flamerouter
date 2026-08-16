package translator

import (
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/formats"
	"strings"
)

type (
	RequestFunc  func(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any
	ResponseFunc func(chunk map[string]any, state *concerns.ResponseState) []map[string]any
)

type Registry struct {
	requestFns  map[string]RequestFunc
	responseFns map[string]ResponseFunc
}

func NewRegistry() *Registry {
	return &Registry{
		requestFns:  make(map[string]RequestFunc),
		responseFns: make(map[string]ResponseFunc),
	}
}

var DefaultRegistry = NewRegistry()

func Register(from, to string, reqFn RequestFunc, resFn ResponseFunc) {
	key := from + ":" + to

	if reqFn != nil {
		DefaultRegistry.requestFns[key] = reqFn
	}

	if resFn != nil {
		DefaultRegistry.responseFns[key] = resFn
	}
}

type TranslateOptions struct {
	ClientTool   any
	Credentials  map[string]any
	Model        string
	Provider     string
	ConnectionId string
	StripList    []string
	Stream       bool
}

func (r *Registry) TranslateRequest(sourceFormat, targetFormat string, body map[string]any, opts TranslateOptions) map[string]any {
	result := body

	if len(opts.StripList) > 0 {
		concerns.StripContentTypes(result, opts.StripList)
	}

	// Pre-translate: strip media + prefetch images (RTK full suite runs post-translate in chat)
	caps := concerns.GetCapabilitiesForModel(opts.Provider, opts.Model)
	if caps != nil {
		concerns.StripUnsupportedModalities(result, sourceFormat, caps)
	}

	concerns.PrefetchRemoteImages(result, sourceFormat, targetFormat)

	concerns.NormalizeThinkingConfig(result)

	concerns.EnsureToolCallIds(result)
	concerns.FixMissingToolResponses(result)

	thinkingIntent := concerns.CaptureThinking(result)

	clientSessionId := concerns.CaptureSessionId(result, opts.Credentials, opts.ConnectionId, targetFormat)
	if opts.Credentials != nil {
		opts.Credentials["_clientSessionId"] = clientSessionId
	}

	if sourceFormat != targetFormat {
		key := sourceFormat + ":" + targetFormat
		if directFn, ok := r.requestFns[key]; ok {
			result = directFn(opts.Model, result, opts.Stream, opts.Credentials)
		} else {
			if sourceFormat != FormatOpenAI {
				toOpenAI := r.requestFns[sourceFormat+":"+FormatOpenAI]
				if toOpenAI != nil {
					result = toOpenAI(opts.Model, result, opts.Stream, opts.Credentials)
				}
			}

			if targetFormat != FormatOpenAI {
				fromOpenAI := r.requestFns[FormatOpenAI+":"+targetFormat]
				if fromOpenAI != nil {
					result = fromOpenAI(opts.Model, result, opts.Stream, opts.Credentials)
				}
			}
		}
	}

	if targetFormat == FormatOpenAI {
		result = filterToOpenAIFormat(result)
	}

	if targetFormat == FormatClaude {
		apiKey := ""

		if opts.Credentials != nil {
			if at, ok := opts.Credentials["accessToken"].(string); ok {
				apiKey = at
			} else if ak, ok := opts.Credentials["apiKey"].(string); ok {
				apiKey = ak
			}
		}

		rawHeaders := ""

		if opts.Credentials != nil {
			if rh, ok := opts.Credentials["rawHeaders"].(string); ok {
				rawHeaders = rh
			}
		}

		result = prepareClaudeRequest(result, opts.Provider, apiKey, opts.ConnectionId, rawHeaders, clientSessionId)
	}

	if thinkingIntent != nil || opts.Model != "" {
		result = concerns.ApplyThinking(targetFormat, opts.Model, result, opts.Provider, thinkingIntent)
	}

	if opts.Provider != "" && isCloakToolsOnOAuth(opts.Provider) {
		apiKey := ""

		if opts.Credentials != nil {
			if at, ok := opts.Credentials["accessToken"].(string); ok {
				apiKey = at
			} else if ak, ok := opts.Credentials["apiKey"].(string); ok {
				apiKey = ak
			}
		}

		if strings.Contains(apiKey, "sk-ant-oat") {
			cloakedBody, toolNameMap := cloakClaudeTools(result)
			result = cloakedBody

			if len(toolNameMap) > 0 {
				result["_toolNameMap"] = toolNameMap
			}
		}
	}

	return result
}

func (r *Registry) TranslateResponse(targetFormat, sourceFormat string, chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if sourceFormat == targetFormat {
		return []map[string]any{chunk}
	}

	key := targetFormat + ":" + sourceFormat
	if directFn, ok := r.responseFns[key]; ok {
		result := directFn(chunk, state)
		if result == nil {
			return nil
		}

		return result
	}

	var results []map[string]any

	if targetFormat != FormatOpenAI {
		toOpenAI := r.responseFns[targetFormat+":"+FormatOpenAI]
		if toOpenAI != nil {
			results = toOpenAI(chunk, state)
		}
	} else {
		results = []map[string]any{chunk}
	}

	if sourceFormat != FormatOpenAI {
		fromOpenAI := r.responseFns[FormatOpenAI+":"+sourceFormat]
		if fromOpenAI != nil {
			var finalResults []map[string]any

			for _, r := range results {
				converted := fromOpenAI(r, state)
				if converted != nil {
					finalResults = append(finalResults, converted...)
				}
			}

			results = finalResults
		}
	}

	return results
}

func NeedsTranslation(sourceFormat, targetFormat string) bool {
	return sourceFormat != targetFormat
}

func InitState(sourceFormat string) *concerns.ResponseState {
	return concerns.NewResponseState()
}

func getCapabilitiesForModel(provider, model string) *concerns.Capabilities {
	return concerns.GetCapabilitiesForModel(provider, model)
}

func filterToOpenAIFormat(body map[string]any) map[string]any {
	if body == nil || body["messages"] == nil {
		return body
	}

	messages, ok := body["messages"].([]any)
	if !ok {
		return body
	}

	var filteredMessages []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			filteredMessages = append(filteredMessages, msgRaw)
			continue
		}

		role, _ := msg["role"].(string)

		if role == "developer" {
			msg = map[string]any{
				"role":    "system",
				"content": msg["content"],
			}
			role = "system"
		}

		if role == "tool" {
			filteredMessages = append(filteredMessages, msg)
			continue
		}

		if role == "assistant" {
			if _, hasTC := msg["tool_calls"]; hasTC {
				filteredMessages = append(filteredMessages, msg)
				continue
			}
		}

		content, ok := msg["content"]
		if !ok {
			filteredMessages = append(filteredMessages, msg)
			continue
		}

		if _, isStr := content.(string); isStr {
			filteredMessages = append(filteredMessages, msg)
			continue
		}

		contentArr, ok := content.([]any)
		if !ok {
			filteredMessages = append(filteredMessages, msg)
			continue
		}

		var newContent []any

		for _, blockRaw := range contentArr {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				newContent = append(newContent, blockRaw)
				continue
			}

			btype, _ := block["type"].(string)

			if btype == "thinking" || btype == "redacted_thinking" {
				continue
			}

			validOpenAI := map[string]bool{
				"text": true, "image_url": true, "image": true,
				"input_audio": true, "audio_url": true, "file": true,
			}
			if validOpenAI[btype] {
				stripped := stripBlock(block)
				newContent = append(newContent, stripped)
			} else if btype == "tool_use" {
				continue
			} else if btype == "tool_result" {
				stripped := stripBlock(block)
				newContent = append(newContent, stripped)
			}
		}

		if len(newContent) == 0 {
			newContent = append(newContent, map[string]any{"type": "text", "text": ""})
		}

		msg["content"] = newContent
		filteredMessages = append(filteredMessages, msg)
	}

	var finalMessages []any

	for _, msgRaw := range filteredMessages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			finalMessages = append(finalMessages, msgRaw)
			continue
		}

		role, _ := msg["role"].(string)
		if role == "tool" {
			finalMessages = append(finalMessages, msg)
			continue
		}

		if role == "assistant" {
			if _, hasTC := msg["tool_calls"]; hasTC {
				finalMessages = append(finalMessages, msg)
				continue
			}
		}

		content := msg["content"]
		if _, isStr := content.(string); isStr {
			finalMessages = append(finalMessages, msg)
			continue
		}

		contentArr, ok := content.([]any)
		if !ok {
			finalMessages = append(finalMessages, msg)
			continue
		}

		hasContent := false

		for _, b := range contentArr {
			block, ok := b.(map[string]any)
			if !ok {
				hasContent = true
				break
			}

			btype, _ := block["type"].(string)
			if btype == "text" {
				if text, ok := block["text"].(string); ok && text != "" {
					hasContent = true
					break
				}
			} else if btype != "text" {
				hasContent = true
				break
			}
		}

		if hasContent {
			finalMessages = append(finalMessages, msg)
		}
	}

	body["messages"] = finalMessages

	if tools, ok := body["tools"].([]any); ok && len(tools) == 0 {
		delete(body, "tools")
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		var normalizedTools []any

		for _, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]any)
			if !ok {
				normalizedTools = append(normalizedTools, toolRaw)
				continue
			}

			if t, ok := tool["type"].(string); ok && t == "function" {
				if _, hasFn := tool["function"]; hasFn {
					normalizedTools = append(normalizedTools, tool)
					continue
				}
			}

			if name, ok := tool["name"].(string); ok {
				if _, hasSchema := tool["input_schema"]; hasSchema {
					desc, _ := tool["description"].(string)
					normalizedTools = append(normalizedTools, map[string]any{
						"type": "function",
						"function": map[string]any{
							"name":        name,
							"description": desc,
							"parameters":  tool["input_schema"],
						},
					})

					continue
				}
			}

			normalizedTools = append(normalizedTools, tool)
		}

		body["tools"] = normalizedTools
	}

	if tc, ok := body["tool_choice"].(map[string]any); ok {
		tctype, _ := tc["type"].(string)
		if tctype == "auto" {
			body["tool_choice"] = "auto"
		} else if tctype == "any" {
			body["tool_choice"] = "required"
		} else if tctype == "tool" {
			if name, ok := tc["name"].(string); ok {
				body["tool_choice"] = map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				}
			}
		}
	}

	return body
}

func stripBlock(block map[string]any) map[string]any {
	result := make(map[string]any)

	for k, v := range block {
		if k == "signature" {
			continue
		}

		result[k] = v
	}

	return result
}

func handlesThinkingBlocks(provider string) bool {
	return provider == "claude" || strings.HasPrefix(provider, "anthropic-compatible") || provider == "deepseek"
}

func prepareClaudeRequest(body map[string]any, provider, apiKey, connectionId, rawHeaders, sessionId string) map[string]any {
	if body == nil {
		return body
	}

	model, _ := body["model"].(string)
	body = formats.NormalizeClaudePassthrough(body, model)

	// MiniMax quirk: drop output_config
	if provider == "minimax" || provider == "minimax-cn" {
		delete(body, "output_config")
	}

	if body["max_tokens"] != nil {
		ceiling := concerns.DefaultMaxTokens
		if caps := getCapabilitiesForModel(provider, model); caps != nil && caps.MaxOutput > 0 {
			ceiling = caps.MaxOutput
		}

		mt := 0
		switch v := body["max_tokens"].(type) {
		case float64:
			mt = int(v)
		case int:
			mt = v
		}

		if mt > ceiling {
			mt = ceiling
			body["max_tokens"] = mt
		}

		if thinking, ok := body["thinking"].(map[string]any); ok {
			if ttype, _ := thinking["type"].(string); ttype == "enabled" {
				bt := 0
				switch v := thinking["budget_tokens"].(type) {
				case float64:
					bt = int(v)
				case int:
					bt = v
				}

				if bt > 0 && bt >= mt {
					newMt := bt + 1024
					if newMt > ceiling {
						newMt = ceiling
					}

					body["max_tokens"] = newMt

					mt = newMt
					if bt >= mt {
						newBt := mt - 1024
						if newBt < 1024 {
							newBt = 1024
						}

						thinking["budget_tokens"] = newBt
					}
				}
			}
		}
	}

	if system, ok := body["system"].([]any); ok {
		var newSystem []any

		for i, blockRaw := range system {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				newSystem = append(newSystem, blockRaw)
				continue
			}

			stripped := make(map[string]any)

			for k, v := range block {
				if k == "cache_control" {
					continue
				}

				stripped[k] = v
			}

			if i == len(system)-1 {
				stripped["cache_control"] = map[string]any{"type": "ephemeral", "ttl": "1h"}
			}

			newSystem = append(newSystem, stripped)
		}

		body["system"] = newSystem
	}

	if messages, ok := body["messages"].([]any); ok {
		lenMsgs := len(messages)

		filtered := make([]any, 0, lenMsgs)

		for i, msgRaw := range messages {
			msg, ok := msgRaw.(map[string]any)
			if !ok {
				filtered = append(filtered, msgRaw)
				continue
			}

			if contentArr, ok := msg["content"].([]any); ok {
				for _, blockRaw := range contentArr {
					if block, ok := blockRaw.(map[string]any); ok {
						delete(block, "cache_control")
					}
				}
			}
			// Keep final assistant even if empty
			role, _ := msg["role"].(string)

			isFinalAssistant := i == lenMsgs-1 && role == "assistant"
			if isFinalAssistant || formats.HasValidContent(msg) {
				filtered = append(filtered, msg)
			}
		}

		filtered = formats.FixToolUseOrdering(filtered)

		// Thinking enabled + last message is user
		lastMessageIsUser := false

		if len(filtered) > 0 {
			if last, ok := filtered[len(filtered)-1].(map[string]any); ok {
				if role, _ := last["role"].(string); role == "user" {
					lastMessageIsUser = true
				}
			}
		}

		thinkingEnabled := false

		if t, ok := body["thinking"].(map[string]any); ok {
			if tt, _ := t["type"].(string); tt == "enabled" && lastMessageIsUser {
				thinkingEnabled = true
			}
		}

		// Reverse pass: cache_control on last assistant + thinking block handling
		lastAssistantProcessed := false

		for i := len(filtered) - 1; i >= 0; i-- {
			msg, ok := filtered[i].(map[string]any)
			if !ok {
				continue
			}

			role, _ := msg["role"].(string)
			if role != "assistant" {
				continue
			}

			content, ok := msg["content"].([]any)
			if !ok {
				continue
			}

			if !lastAssistantProcessed && len(content) > 0 {
				for j := len(content) - 1; j >= 0; j-- {
					block, ok := content[j].(map[string]any)
					if !ok {
						continue
					}

					bt, _ := block["type"].(string)
					if bt != "thinking" && bt != "redacted_thinking" {
						block["cache_control"] = map[string]any{"type": "ephemeral"}
						break
					}
				}

				lastAssistantProcessed = true
			}

			if handlesThinkingBlocks(provider) {
				isClaudeNative := provider == "claude"
				isDeepSeek := provider == "deepseek"
				hasToolUse := false
				hasKeptThinking := false

				var kept []any

				for _, blockRaw := range content {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						kept = append(kept, blockRaw)
						continue
					}

					bt, _ := block["type"].(string)

					isThinking := bt == "thinking" || bt == "redacted_thinking"
					if isThinking {
						if isClaudeNative {
							sig, _ := block["signature"].(string)
							if formats.IsValidClaudeSignature(sig) {
								hasKeptThinking = true

								kept = append(kept, block)
							}
						} else if isDeepSeek {
							hasKeptThinking = true

							kept = append(kept, block)
						} else {
							block["signature"] = formats.DefaultThinkingClaudeSignature
							hasKeptThinking = true

							kept = append(kept, block)
						}

						continue
					}

					if bt == "tool_use" {
						hasToolUse = true
					}

					kept = append(kept, block)
				}

				msg["content"] = kept

				if thinkingEnabled && !hasKeptThinking && hasToolUse {
					placeholder := map[string]any{"type": "thinking", "thinking": "."}
					if !isDeepSeek {
						placeholder["signature"] = formats.DefaultThinkingClaudeSignature
					}

					msg["content"] = append([]any{placeholder}, kept...)
				}
			}
		}

		body["messages"] = filtered
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		// Strip built-in tools for non-Anthropic; normalize function shape
		if provider != "claude" {
			var normalized []any

			for _, toolRaw := range tools {
				tool, ok := toolRaw.(map[string]any)
				if !ok {
					continue
				}

				if t, ok := tool["type"].(string); ok && t != "" && t != "function" {
					continue
				}

				if fn, ok := tool["function"].(map[string]any); ok {
					normalized = append(normalized, map[string]any{
						"name":         fn["name"],
						"description":  fn["description"],
						"input_schema": fn["parameters"],
					})

					continue
				}

				newTool := make(map[string]any)

				for k, v := range tool {
					if k == "type" {
						continue
					}

					newTool[k] = v
				}

				normalized = append(normalized, newTool)
			}

			tools = normalized
		}

		var cleanedTools []any

		for i, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]any)
			if !ok {
				cleanedTools = append(cleanedTools, toolRaw)
				continue
			}

			newTool := make(map[string]any)

			for k, v := range tool {
				if k == "cache_control" {
					continue
				}

				newTool[k] = v
			}

			if i == len(tools)-1 {
				newTool["cache_control"] = map[string]any{"type": "ephemeral", "ttl": "1h"}
			}

			cleanedTools = append(cleanedTools, newTool)
		}

		if len(cleanedTools) == 0 {
			delete(body, "tools")
			delete(body, "tool_choice")
		} else {
			body["tools"] = cleanedTools
		}
	}

	// Apply cloaking for OAuth tokens
	if (provider == "claude" || strings.HasPrefix(provider, "anthropic-compatible")) && apiKey != "" {
		body = formats.ApplyCloaking(body, apiKey, sessionId)
	}

	return body
}

func isCloakToolsOnOAuth(provider string) bool {
	cloakProviders := map[string]bool{
		"claude":     true,
		"claudecode": true,
	}

	return cloakProviders[provider]
}

var ccDecoyTools = []map[string]any{
	{"name": "Task", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskOutput", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskStop", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskCreate", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskGet", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskUpdate", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "TaskList", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Bash", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Glob", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Grep", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Read", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Edit", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Write", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "NotebookEdit", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "WebFetch", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "WebSearch", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "AskUserQuestion", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "Skill", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "EnterPlanMode", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "ExitPlanMode", "description": "This tool is currently unavailable.", "input_schema": map[string]any{"type": "object", "properties": map[string]any{}}},
}

const claudeToolSuffix = "_cc"

func cloakClaudeTools(body map[string]any) (map[string]any, map[string]string) {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body, nil
	}

	toolNameMap := make(map[string]string)
	clientToolNames := make(map[string]bool)

	var clientDeclarations []any

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			clientDeclarations = append(clientDeclarations, toolRaw)
			continue
		}

		if _, hasType := tool["type"]; hasType {
			clientDeclarations = append(clientDeclarations, tool)
			continue
		}

		name, _ := tool["name"].(string)
		if name == "" {
			clientDeclarations = append(clientDeclarations, tool)
			continue
		}

		suffixed := name + claudeToolSuffix
		toolNameMap[suffixed] = name
		clientToolNames[name] = true

		newTool := make(map[string]any)
		for k, v := range tool {
			newTool[k] = v
		}

		newTool["name"] = suffixed
		clientDeclarations = append(clientDeclarations, newTool)
	}

	var allTools []any
	allTools = append(allTools, clientDeclarations...)

	for _, t := range ccDecoyTools {
		allTools = append(allTools, t)
	}

	var renamedMessages []any

	if messages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range messages {
			msg, ok := msgRaw.(map[string]any)
			if !ok {
				renamedMessages = append(renamedMessages, msgRaw)
				continue
			}

			contentArr, ok := msg["content"].([]any)
			if !ok {
				renamedMessages = append(renamedMessages, msg)
				continue
			}

			var renamedContent []any

			for _, blockRaw := range contentArr {
				block, ok := blockRaw.(map[string]any)
				if !ok {
					renamedContent = append(renamedContent, blockRaw)
					continue
				}

				if btype, _ := block["type"].(string); btype == "tool_use" {
					if name, ok := block["name"].(string); ok {
						newBlock := make(map[string]any)
						for k, v := range block {
							newBlock[k] = v
						}

						newBlock["name"] = name + claudeToolSuffix
						renamedContent = append(renamedContent, newBlock)

						continue
					}
				}

				renamedContent = append(renamedContent, block)
			}

			newMsg := make(map[string]any)
			for k, v := range msg {
				newMsg[k] = v
			}

			newMsg["content"] = renamedContent
			renamedMessages = append(renamedMessages, newMsg)
		}
	}

	cloakedBody := make(map[string]any)
	for k, v := range body {
		cloakedBody[k] = v
	}

	cloakedBody["tools"] = allTools
	if renamedMessages != nil {
		cloakedBody["messages"] = renamedMessages
	}

	if tc, ok := body["tool_choice"].(map[string]any); ok {
		if tctype, ok := tc["type"].(string); ok && tctype == "tool" {
			if name, ok := tc["name"].(string); ok && clientToolNames[name] {
				newTC := make(map[string]any)
				for k, v := range tc {
					newTC[k] = v
				}

				newTC["name"] = name + claudeToolSuffix
				cloakedBody["tool_choice"] = newTC
			}
		}
	}

	var toolNameMapPtr map[string]string
	if len(toolNameMap) > 0 {
		toolNameMapPtr = toolNameMap
	}

	return cloakedBody, toolNameMapPtr
}

func decloakToolNames(body map[string]any, toolNameMap map[string]string) map[string]any {
	if len(toolNameMap) == 0 || body == nil {
		return body
	}

	contentArr, ok := body["content"].([]any)
	if !ok {
		return body
	}

	var newContent []any

	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			newContent = append(newContent, blockRaw)
			continue
		}

		if btype, _ := block["type"].(string); btype == "tool_use" {
			if name, ok := block["name"].(string); ok {
				if original, exists := toolNameMap[name]; exists {
					newBlock := make(map[string]any)
					for k, v := range block {
						newBlock[k] = v
					}

					newBlock["name"] = original
					newContent = append(newContent, newBlock)

					continue
				}
			}
		}

		newContent = append(newContent, block)
	}

	result := make(map[string]any)
	for k, v := range body {
		result[k] = v
	}

	result["content"] = newContent

	return result
}
