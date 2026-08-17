// Package translator provides core translator registries, types, and format detection logic.
package translator

import (
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/formats"
	"strings"
)

// RequestFunc defines the function signature for request payload translators.
type RequestFunc func(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any

// ResponseFunc defines the function signature for response streaming translators.
type ResponseFunc func(chunk map[string]any, state *concerns.ResponseState) []map[string]any

// Registry manages registered request and response translators.
type Registry struct {
	requestFns  map[string]RequestFunc
	responseFns map[string]ResponseFunc
}

// NewRegistry creates a new initialized Registry instance.
func NewRegistry() *Registry {
	return &Registry{
		requestFns:  make(map[string]RequestFunc),
		responseFns: make(map[string]ResponseFunc),
	}
}

// DefaultRegistry is the default singleton translation registry.
var DefaultRegistry = NewRegistry()

// Register registers a request and/or response translator pair between two formats.
func Register(from, to string, reqFn RequestFunc, resFn ResponseFunc) {
	key := from + ":" + to

	if reqFn != nil {
		DefaultRegistry.requestFns[key] = reqFn
	}

	if resFn != nil {
		DefaultRegistry.responseFns[key] = resFn
	}
}

// TranslateOptions configures translation execution settings and context.
type TranslateOptions struct {
	ClientTool   any
	Credentials  map[string]any
	Model        string
	Provider     string
	ConnectionID string
	StripList    []string
	Stream       bool
}

func extractCredentialField(credentials map[string]any, keys ...string) string {
	if credentials == nil {
		return ""
	}

	for _, k := range keys {
		if val, ok := credentials[k].(string); ok && val != "" {
			return val
		}
	}

	return ""
}

func (r *Registry) executeDirectOrBridgedTranslation(
	sourceFormat, targetFormat string,
	body map[string]any,
	opts TranslateOptions,
) map[string]any {
	if sourceFormat == targetFormat {
		return body
	}

	key := sourceFormat + ":" + targetFormat
	if directFn, ok := r.requestFns[key]; ok {
		return directFn(opts.Model, body, opts.Stream, opts.Credentials)
	}

	result := body

	if sourceFormat != FormatOpenAI {
		if toOpenAI := r.requestFns[sourceFormat+":"+FormatOpenAI]; toOpenAI != nil {
			result = toOpenAI(opts.Model, result, opts.Stream, opts.Credentials)
		}
	}

	if targetFormat != FormatOpenAI {
		if fromOpenAI := r.requestFns[FormatOpenAI+":"+targetFormat]; fromOpenAI != nil {
			result = fromOpenAI(opts.Model, result, opts.Stream, opts.Credentials)
		}
	}

	return result
}

func (r *Registry) applyClaudePostPipeline(
	result map[string]any,
	opts TranslateOptions,
	clientSessionID string,
) map[string]any {
	apiKey := extractCredentialField(opts.Credentials, "accessToken", "apiKey")
	rawHeaders := extractCredentialField(opts.Credentials, "rawHeaders")

	return prepareClaudeRequest(result, opts.Provider, apiKey, rawHeaders, clientSessionID)
}

func (r *Registry) applyThinkingIfEnabled(
	targetFormat string,
	result map[string]any,
	opts TranslateOptions,
	thinkingIntent any,
) map[string]any {
	if thinkingIntent == nil && opts.Model == "" {
		return result
	}

	var intentMap map[string]any

	if thinkingIntent != nil {
		if m, ok := thinkingIntent.(map[string]any); ok {
			intentMap = m
		}
	}

	return concerns.ApplyThinking(targetFormat, opts.Model, result, opts.Provider, intentMap)
}

func (r *Registry) applyOAuthCloakingIfApplicable(result map[string]any, opts TranslateOptions) map[string]any {
	if opts.Provider == "" || !isCloakToolsOnOAuth(opts.Provider) {
		return result
	}

	apiKey := extractCredentialField(opts.Credentials, "accessToken", "apiKey")
	if !strings.Contains(apiKey, "sk-ant-oat") {
		return result
	}

	cloakedBody, toolNameMap := cloakClaudeTools(result)
	if len(toolNameMap) > 0 {
		cloakedBody["_toolNameMap"] = toolNameMap
	}

	return cloakedBody
}

func (r *Registry) applyPostTranslationPipeline(
	targetFormat string,
	result map[string]any,
	opts TranslateOptions,
	thinkingIntent any,
	clientSessionID string,
) map[string]any {
	if targetFormat == FormatOpenAI {
		result = filterToOpenAIFormat(result)
	}

	if targetFormat == FormatClaude {
		result = r.applyClaudePostPipeline(result, opts, clientSessionID)
	}

	result = r.applyThinkingIfEnabled(targetFormat, result, opts, thinkingIntent)

	return r.applyOAuthCloakingIfApplicable(result, opts)
}

// TranslateRequest executes request payload transformation through registered converters.
func (r *Registry) TranslateRequest(sourceFormat, targetFormat string, body map[string]any, opts TranslateOptions) map[string]any {
	result := body

	if len(opts.StripList) > 0 {
		concerns.StripContentTypes(result, opts.StripList)
	}

	caps := concerns.GetCapabilitiesForModel(opts.Provider, opts.Model)
	if caps != nil {
		concerns.StripUnsupportedModalities(result, sourceFormat, caps)
	}

	concerns.PrefetchRemoteImages(result, sourceFormat, targetFormat)
	concerns.NormalizeThinkingConfig(result)
	concerns.EnsureToolCallIDs(result)
	concerns.FixMissingToolResponses(result)

	thinkingIntent := concerns.CaptureThinking(result)
	clientSessionID := concerns.CaptureSessionID(result, opts.Credentials, opts.ConnectionID, targetFormat)

	if opts.Credentials != nil {
		opts.Credentials["_clientSessionId"] = clientSessionID
	}

	result = r.executeDirectOrBridgedTranslation(sourceFormat, targetFormat, result, opts)

	return r.applyPostTranslationPipeline(targetFormat, result, opts, thinkingIntent, clientSessionID)
}

// TranslateResponse executes response chunk transformation through registered converters.
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
		if toOpenAI := r.responseFns[targetFormat+":"+FormatOpenAI]; toOpenAI != nil {
			results = toOpenAI(chunk, state)
		}
	} else {
		results = []map[string]any{chunk}
	}

	if sourceFormat != FormatOpenAI {
		if fromOpenAI := r.responseFns[FormatOpenAI+":"+sourceFormat]; fromOpenAI != nil {
			var finalResults []map[string]any

			for _, res := range results {
				if converted := fromOpenAI(res, state); converted != nil {
					finalResults = append(finalResults, converted...)
				}
			}

			results = finalResults
		}
	}

	return results
}

// NeedsTranslation returns whether source and target formats differ.
func NeedsTranslation(sourceFormat, targetFormat string) bool {
	return sourceFormat != targetFormat
}

// InitState initializes response transformation state.
func InitState(_ string) *concerns.ResponseState {
	return concerns.NewResponseState()
}

func getCapabilitiesForModel(provider, model string) *concerns.Capabilities {
	return concerns.GetCapabilitiesForModel(provider, model)
}

func normalizeDeveloperMessage(msg map[string]any) (map[string]any, string) {
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	if role == "developer" {
		return map[string]any{
			"role":    "system",
			"content": msg["content"],
		}, "system"
	}

	return msg, role
}

func isOpenAIBlockValid(btype string) bool {
	validOpenAI := map[string]bool{
		"text":        true,
		"image_url":   true,
		"image":       true,
		"input_audio": true,
		"audio_url":   true,
		"file":        true,
	}

	return validOpenAI[btype]
}

func filterOpenAIContentBlocks(contentArr []any) []any {
	var newContent []any

	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			newContent = append(newContent, blockRaw)
			continue
		}

		btype, okType := block["type"].(string)
		if !okType || btype == "thinking" || btype == "redacted_thinking" || btype == "tool_use" {
			continue
		}

		if isOpenAIBlockValid(btype) || btype == "tool_result" {
			stripped := stripBlock(block)
			newContent = append(newContent, stripped)
		}
	}

	if len(newContent) == 0 {
		newContent = append(newContent, map[string]any{"type": "text", "text": ""})
	}

	return newContent
}

func filterSingleOpenAIMessage(msgRaw any) (map[string]any, bool) {
	msg, ok := msgRaw.(map[string]any)
	if !ok || msg == nil {
		return nil, false
	}

	msg, role := normalizeDeveloperMessage(msg)
	if role == "tool" {
		return msg, true
	}

	if role == "assistant" {
		if _, hasTC := msg["tool_calls"]; hasTC {
			return msg, true
		}
	}

	content, ok := msg["content"]
	if !ok {
		return msg, true
	}

	if _, isStr := content.(string); isStr {
		return msg, true
	}

	contentArr, ok := content.([]any)
	if !ok {
		return msg, true
	}

	newMsg := make(map[string]any)
	for k, v := range msg {
		newMsg[k] = v
	}

	newMsg["content"] = filterOpenAIContentBlocks(contentArr)

	return newMsg, true
}

func messageBlockHasText(b any) bool {
	block, okBlock := b.(map[string]any)
	if !okBlock || block == nil {
		return true
	}

	btype, okType := block["type"].(string)
	if !okType || btype != "text" {
		return true
	}

	text, okText := block["text"].(string)
	if !okText {
		return false
	}

	return text != ""
}

func messageHasContent(msg map[string]any) bool {
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	if role == "tool" {
		return true
	}

	if role == "assistant" {
		if _, hasTC := msg["tool_calls"]; hasTC {
			return true
		}
	}

	content, ok := msg["content"]
	if !ok {
		return false
	}

	if _, isStr := content.(string); isStr {
		return true
	}

	contentArr, ok := content.([]any)
	if !ok {
		return false
	}

	for _, b := range contentArr {
		if messageBlockHasText(b) {
			return true
		}
	}

	return false
}

func normalizeSingleOpenAITool(toolRaw any) any {
	tool, ok := toolRaw.(map[string]any)
	if !ok || tool == nil {
		return toolRaw
	}

	if t, ok := tool["type"].(string); ok && t == "function" {
		if _, hasFn := tool["function"]; hasFn {
			return tool
		}
	}

	if name, ok := tool["name"].(string); ok {
		if _, hasSchema := tool["input_schema"]; hasSchema {
			desc, okDesc := tool["description"].(string)
			if !okDesc {
				desc = ""
			}

			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        name,
					"description": desc,
					"parameters":  tool["input_schema"],
				},
			}
		}
	}

	return tool
}

func normalizeOpenAIToolChoice(body map[string]any) {
	tc, ok := body["tool_choice"].(map[string]any)
	if !ok || tc == nil {
		return
	}

	tctype, okType := tc["type"].(string)
	if !okType {
		return
	}

	switch tctype {
	case "auto":
		body["tool_choice"] = "auto"
	case "any":
		body["tool_choice"] = "required"
	case "tool":
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

func filterValidOpenAIMessages(messages []any) []any {
	var filteredMessages []any

	for _, msgRaw := range messages {
		if filtered, ok := filterSingleOpenAIMessage(msgRaw); ok {
			filteredMessages = append(filteredMessages, filtered)
		} else {
			filteredMessages = append(filteredMessages, msgRaw)
		}
	}

	var finalMessages []any

	for _, msgRaw := range filteredMessages {
		if msg, ok := msgRaw.(map[string]any); ok && msg != nil {
			if messageHasContent(msg) {
				finalMessages = append(finalMessages, msg)
			}
		}
	}

	return finalMessages
}

func normalizeOpenAITools(body map[string]any) {
	tools, ok := body["tools"].([]any)
	if !ok {
		return
	}

	if len(tools) == 0 {
		delete(body, "tools")
		return
	}

	normalizedTools := make([]any, 0, len(tools))
	for _, toolRaw := range tools {
		normalizedTools = append(normalizedTools, normalizeSingleOpenAITool(toolRaw))
	}

	body["tools"] = normalizedTools
}

func filterToOpenAIFormat(body map[string]any) map[string]any {
	if body == nil || body["messages"] == nil {
		return body
	}

	messages, ok := body["messages"].([]any)
	if !ok {
		return body
	}

	body["messages"] = filterValidOpenAIMessages(messages)
	normalizeOpenAITools(body)
	normalizeOpenAIToolChoice(body)

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

func parseMaxTokens(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	default:
		return 0
	}
}

func calculateBalancedMaxTokens(bt, mt, ceiling int) int {
	if bt <= 0 || bt < mt {
		return mt
	}

	newMt := bt + 1024
	if newMt > ceiling {
		return ceiling
	}

	return newMt
}

func calculateBalancedBudgetTokens(bt, mt int) int {
	if bt <= 0 || bt < mt {
		return bt
	}

	newBt := mt - 1024
	if newBt < 1024 {
		return 1024
	}

	return newBt
}

func balanceThinkingBudget(thinking map[string]any, mt, ceiling int) int {
	ttype, okType := thinking["type"].(string)
	if !okType || ttype != "enabled" {
		return mt
	}

	bt := parseMaxTokens(thinking["budget_tokens"])
	if bt <= 0 || bt < mt {
		return mt
	}

	newMt := calculateBalancedMaxTokens(bt, mt, ceiling)
	newBt := calculateBalancedBudgetTokens(bt, newMt)
	thinking["budget_tokens"] = newBt

	return newMt
}

func adjustMaxTokensAndThinkingBudget(body map[string]any, provider, model string) {
	if body["max_tokens"] == nil {
		return
	}

	ceiling := concerns.DefaultMaxTokens
	if caps := getCapabilitiesForModel(provider, model); caps != nil && caps.MaxOutput > 0 {
		ceiling = caps.MaxOutput
	}

	mt := parseMaxTokens(body["max_tokens"])
	if mt > ceiling {
		mt = ceiling
	}

	if thinking, ok := body["thinking"].(map[string]any); ok && thinking != nil {
		mt = balanceThinkingBudget(thinking, mt, ceiling)
	}

	body["max_tokens"] = mt
}

func prepareClaudeSystem(body map[string]any) {
	system, ok := body["system"].([]any)
	if !ok {
		return
	}

	newSystem := make([]any, 0, len(system))

	for i, blockRaw := range system {
		block, okBlock := blockRaw.(map[string]any)
		if !okBlock {
			newSystem = append(newSystem, blockRaw)
			continue
		}

		stripped := make(map[string]any)

		for k, v := range block {
			if k != "cache_control" {
				stripped[k] = v
			}
		}

		if i == len(system)-1 {
			stripped["cache_control"] = map[string]any{"type": "ephemeral", "ttl": "1h"}
		}

		newSystem = append(newSystem, stripped)
	}

	body["system"] = newSystem
}

func filterClaudeMessages(messages []any) []any {
	lenMsgs := len(messages)
	filtered := make([]any, 0, lenMsgs)

	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			filtered = append(filtered, msgRaw)
			continue
		}

		if contentArr, okContent := msg["content"].([]any); okContent {
			for _, blockRaw := range contentArr {
				if block, okBlock := blockRaw.(map[string]any); okBlock {
					delete(block, "cache_control")
				}
			}
		}

		role, okRole := msg["role"].(string)
		if !okRole {
			role = ""
		}

		isFinalAssistant := i == lenMsgs-1 && role == "assistant"
		if isFinalAssistant || formats.HasValidContent(msg) {
			filtered = append(filtered, msg)
		}
	}

	return formats.FixToolUseOrdering(filtered)
}

func isClaudeThinkingEnabled(body map[string]any, messages []any) bool {
	if len(messages) == 0 {
		return false
	}

	lastMessageIsUser := false

	if last, ok := messages[len(messages)-1].(map[string]any); ok {
		if role, okRole := last["role"].(string); okRole && role == "user" {
			lastMessageIsUser = true
		}
	}

	if t, ok := body["thinking"].(map[string]any); ok {
		if tt, okTT := t["type"].(string); okTT && tt == "enabled" && lastMessageIsUser {
			return true
		}
	}

	return false
}

func processSingleThinkingBlock(block map[string]any, provider string) (map[string]any, bool) {
	if provider == "claude" {
		sig, okSig := block["signature"].(string)
		if okSig && formats.IsValidClaudeSignature(sig) {
			return block, true
		}

		return nil, false
	}

	if provider == "deepseek" {
		return block, true
	}

	block["signature"] = formats.DefaultThinkingClaudeSignature

	return block, true
}

func appendThinkingPlaceholderIfNeeded(kept []any, isDeepSeek, thinkingEnabled, hasKeptThinking, hasToolUse bool) []any {
	if thinkingEnabled && !hasKeptThinking && hasToolUse {
		placeholder := map[string]any{"type": "thinking", "thinking": "."}
		if !isDeepSeek {
			placeholder["signature"] = formats.DefaultThinkingClaudeSignature
		}

		return append([]any{placeholder}, kept...)
	}

	return kept
}

func processClaudeAssistantThinking(msg map[string]any, provider string, thinkingEnabled bool) {
	content, ok := msg["content"].([]any)
	if !ok || len(content) == 0 {
		return
	}

	isDeepSeek := provider == "deepseek"
	hasToolUse := false
	hasKeptThinking := false
	kept := make([]any, 0, len(content))

	for _, blockRaw := range content {
		block, okBlock := blockRaw.(map[string]any)
		if !okBlock {
			kept = append(kept, blockRaw)
			continue
		}

		bt, okType := block["type"].(string)
		if !okType {
			kept = append(kept, block)
			continue
		}

		if bt == "thinking" || bt == "redacted_thinking" {
			if processed, okProc := processSingleThinkingBlock(block, provider); okProc {
				hasKeptThinking = true

				kept = append(kept, processed)
			}

			continue
		}

		if bt == "tool_use" {
			hasToolUse = true
		}

		kept = append(kept, block)
	}

	msg["content"] = appendThinkingPlaceholderIfNeeded(kept, isDeepSeek, thinkingEnabled, hasKeptThinking, hasToolUse)
}

func applyAssistantCacheControl(content []any) {
	for j := len(content) - 1; j >= 0; j-- {
		if block, okBlock := content[j].(map[string]any); okBlock {
			bt, okType := block["type"].(string)
			if okType && bt != "thinking" && bt != "redacted_thinking" {
				block["cache_control"] = map[string]any{"type": "ephemeral"}
				break
			}
		}
	}
}

func applyClaudeAssistantCacheAndThinking(filtered []any, provider string, thinkingEnabled bool) {
	lastAssistantProcessed := false

	for i := len(filtered) - 1; i >= 0; i-- {
		msg, ok := filtered[i].(map[string]any)
		if !ok {
			continue
		}

		role, okRole := msg["role"].(string)
		if !okRole || role != "assistant" {
			continue
		}

		content, okContent := msg["content"].([]any)
		if !okContent {
			continue
		}

		if !lastAssistantProcessed && len(content) > 0 {
			applyAssistantCacheControl(content)

			lastAssistantProcessed = true
		}

		if handlesThinkingBlocks(provider) {
			processClaudeAssistantThinking(msg, provider, thinkingEnabled)
		}
	}
}

func normalizeNonClaudeTools(tools []any) []any {
	normalized := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			continue
		}

		if t, okType := tool["type"].(string); okType && t != "" && t != "function" {
			continue
		}

		if fn, okFn := tool["function"].(map[string]any); okFn {
			normalized = append(normalized, map[string]any{
				"name":         fn["name"],
				"description":  fn["description"],
				"input_schema": fn["parameters"],
			})

			continue
		}

		newTool := make(map[string]any)

		for k, v := range tool {
			if k != "type" {
				newTool[k] = v
			}
		}

		normalized = append(normalized, newTool)
	}

	return normalized
}

func prepareClaudeTools(body map[string]any, provider string) {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return
	}

	if provider != "claude" {
		tools = normalizeNonClaudeTools(tools)
	}

	cleanedTools := make([]any, 0, len(tools))

	for i, toolRaw := range tools {
		tool, okTool := toolRaw.(map[string]any)
		if !okTool {
			cleanedTools = append(cleanedTools, toolRaw)
			continue
		}

		newTool := make(map[string]any)

		for k, v := range tool {
			if k != "cache_control" {
				newTool[k] = v
			}
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

func prepareClaudeRequest(body map[string]any, provider, apiKey, _ /* rawHeaders */, sessionID string) map[string]any {
	if body == nil {
		return body
	}

	model, okModel := body["model"].(string)
	if !okModel {
		model = ""
	}

	body = formats.NormalizeClaudePassthrough(body, model)

	if provider == "minimax" || provider == "minimax-cn" {
		delete(body, "output_config")
	}

	adjustMaxTokensAndThinkingBudget(body, provider, model)
	prepareClaudeSystem(body)

	if messages, ok := body["messages"].([]any); ok {
		filtered := filterClaudeMessages(messages)
		thinkingEnabled := isClaudeThinkingEnabled(body, filtered)
		applyClaudeAssistantCacheAndThinking(filtered, provider, thinkingEnabled)
		body["messages"] = filtered
	}

	prepareClaudeTools(body, provider)

	if (provider == "claude" || strings.HasPrefix(provider, "anthropic-compatible")) && apiKey != "" {
		body = formats.ApplyCloaking(body, apiKey, sessionID)
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

func cloakSingleClaudeTool(toolRaw any, toolNameMap map[string]string, clientToolNames map[string]bool) (any, bool) {
	tool, ok := toolRaw.(map[string]any)
	if !ok {
		return toolRaw, false
	}

	if _, hasType := tool["type"]; hasType {
		return tool, false
	}

	name, okName := tool["name"].(string)
	if !okName || name == "" {
		return tool, false
	}

	suffixed := name + claudeToolSuffix
	toolNameMap[suffixed] = name
	clientToolNames[name] = true

	newTool := make(map[string]any)
	for k, v := range tool {
		newTool[k] = v
	}

	newTool["name"] = suffixed

	return newTool, true
}

func cloakMessageContent(contentArr []any) []any {
	renamedContent := make([]any, 0, len(contentArr))

	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			renamedContent = append(renamedContent, blockRaw)
			continue
		}

		btype, okType := block["type"].(string)
		if okType && btype == "tool_use" {
			if name, okName := block["name"].(string); okName {
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

	return renamedContent
}

func cloakClaudeMessages(messages []any) []any {
	renamedMessages := make([]any, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			renamedMessages = append(renamedMessages, msgRaw)
			continue
		}

		contentArr, okContent := msg["content"].([]any)
		if !okContent {
			renamedMessages = append(renamedMessages, msg)
			continue
		}

		newMsg := make(map[string]any)
		for k, v := range msg {
			newMsg[k] = v
		}

		newMsg["content"] = cloakMessageContent(contentArr)
		renamedMessages = append(renamedMessages, newMsg)
	}

	return renamedMessages
}

func cloakClaudeToolChoice(tcRaw any, clientToolNames map[string]bool) map[string]any {
	tc, ok := tcRaw.(map[string]any)
	if !ok || tc == nil {
		return nil
	}

	if tctype, okType := tc["type"].(string); okType && tctype == "tool" {
		if name, okName := tc["name"].(string); okName && clientToolNames[name] {
			newTC := make(map[string]any)
			for k, v := range tc {
				newTC[k] = v
			}

			newTC["name"] = name + claudeToolSuffix

			return newTC
		}
	}

	return nil
}

func cloakClaudeTools(body map[string]any) (map[string]any, map[string]string) {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body, nil
	}

	toolNameMap := make(map[string]string)
	clientToolNames := make(map[string]bool)

	clientDeclarations := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		cloaked, _ := cloakSingleClaudeTool(toolRaw, toolNameMap, clientToolNames)
		clientDeclarations = append(clientDeclarations, cloaked)
	}

	allTools := make([]any, 0, len(clientDeclarations)+len(ccDecoyTools))
	allTools = append(allTools, clientDeclarations...)

	for _, t := range ccDecoyTools {
		allTools = append(allTools, t)
	}

	cloakedBody := make(map[string]any)
	for k, v := range body {
		cloakedBody[k] = v
	}

	cloakedBody["tools"] = allTools

	if messages, okMsgs := body["messages"].([]any); okMsgs {
		cloakedBody["messages"] = cloakClaudeMessages(messages)
	}

	if tc := cloakClaudeToolChoice(body["tool_choice"], clientToolNames); tc != nil {
		cloakedBody["tool_choice"] = tc
	}

	var toolNameMapPtr map[string]string
	if len(toolNameMap) > 0 {
		toolNameMapPtr = toolNameMap
	}

	return cloakedBody, toolNameMapPtr
}
