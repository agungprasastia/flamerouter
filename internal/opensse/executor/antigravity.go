package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flamerouter/internal/translator/formats"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)


func init() {
	RegisterSpecialized("antigravity", &AntigravityExecutor{
		Base: Base{
			Provider: "antigravity",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent",
			BaseURLs: []string{"https://daily-cloudcode-pa.googleapis.com"},
		},
	})
}

var antigravityBlacklist = []string{
	"output_config", "thinking", "reasoning_effort", "reasoning",
	"enable_thinking", "thinking_budget", "thinkingConfig",
}

// AntigravityExecutor executes Cloud Code / Antigravity Gemini requests.
type AntigravityExecutor struct {
	Base
}

func (e *AntigravityExecutor) stripBlacklisted(obj map[string]any) {
	for _, k := range antigravityBlacklist {
		delete(obj, k)
	}
}

func cleanSingleDecl(dRaw any, isCamel bool) {
	d, okMap := dRaw.(map[string]any)
	if !okMap {
		return
	}

	if params, okP := d["parameters"].(map[string]any); okP {
		d["parameters"] = formats.CleanJSONSchemaForAntigravity(params)
	}

	if isCamel {
		if name, okN := d["name"].(string); okN {
			d["name"] = sanitizeFunctionName(name)
		}
	}
}

func cleanFunctionDeclarations(t map[string]any) {
	if decls, ok := t["functionDeclarations"].([]any); ok {
		for _, dRaw := range decls {
			cleanSingleDecl(dRaw, true)
		}
	}

	if decls, ok := t["function_declarations"].([]any); ok {
		for _, dRaw := range decls {
			cleanSingleDecl(dRaw, false)
		}
	}
}

var agDefaultTools = map[string]bool{
	"browser_subagent":           true,
	"command_status":             true,
	"find_by_name":               true,
	"generate_image":             true,
	"grep_search":                true,
	"list_dir":                   true,
	"list_resources":             true,
	"multi_replace_file_content": true,
	"notify_user":                true,
	"read_resource":              true,
	"read_terminal":              true,
	"read_url_content":           true,
	"replace_file_content":       true,
	"run_command":                true,
	"search_web":                 true,
	"send_command_input":         true,
	"task_boundary":              true,
	"view_content_chunk":         true,
	"view_file":                  true,
	"write_to_file":              true,
}

var agDecoyTools = []map[string]any{
	{"name": "browser_subagent", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "command_status", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "find_by_name", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "generate_image", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "grep_search", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "list_dir", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "list_resources", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "mcp_sequential-thinking_sequentialthinking", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "multi_replace_file_content", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "notify_user", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "read_resource", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "read_terminal", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "read_url_content", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "replace_file_content", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "run_command", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "search_web", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "send_command_input", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "task_boundary", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "view_content_chunk", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "view_file", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
	{"name": "write_to_file", "description": "This tool is currently unavailable.", "parameters": map[string]any{"type": "OBJECT", "properties": map[string]any{}, "required": []any{}}},
}

const agToolSuffix = "_ide"

func extractClientDeclarations(tools []any, seenNames map[string]bool) []any {
	var clientDecls []any

	for _, tRaw := range tools {
		t, okT := tRaw.(map[string]any)
		if !okT {
			continue
		}

		cleanFunctionDeclarations(t)

		decls, okD := t["functionDeclarations"].([]any)
		if !okD {
			decls, _ = t["function_declarations"].([]any) // nolint:errcheck
		}

		for _, dRaw := range decls {
			decl, okDecl := dRaw.(map[string]any)
			if !okDecl {
				continue
			}

			name, okN := decl["name"].(string)
			if !okN || name == "" {
				continue
			}

			if agDefaultTools[name] {
				clientDecls = append(clientDecls, decl)
				seenNames[name] = true

				continue
			}

			suffixed := name + agToolSuffix
			decl["name"] = suffixed
			clientDecls = append(clientDecls, decl)
			seenNames[suffixed] = true
		}
	}

	return clientDecls
}

func cloakCallMap(m map[string]any) {
	if fnName, okN := m["name"].(string); okN && fnName != "" {
		if !agDefaultTools[fnName] && !strings.HasSuffix(fnName, agToolSuffix) {
			m["name"] = fnName + agToolSuffix
		}
	}
}

func cloakPartFunction(p map[string]any) {
	if fc, okFC := p["functionCall"].(map[string]any); okFC {
		cloakCallMap(fc)
	}

	if fr, okFR := p["functionResponse"].(map[string]any); okFR {
		cloakCallMap(fr)
	}
}

func cloakContentsFunctions(contents []any) {
	for _, cRaw := range contents {
		c, okMsg := cRaw.(map[string]any)
		if !okMsg {
			continue
		}

		parts, okP := c["parts"].([]any)
		if !okP {
			continue
		}

		for _, pRaw := range parts {
			if p, okPart := pRaw.(map[string]any); okPart {
				cloakPartFunction(p)
			}
		}
	}
}

func cloakAntigravityTools(request map[string]any) {
	tools, ok := request["tools"].([]any)
	if !ok || len(tools) == 0 {
		return
	}

	seenNames := make(map[string]bool)
	clientDecls := extractClientDeclarations(tools, seenNames)

	allDecls := make([]any, 0, len(clientDecls)+len(agDecoyTools))
	allDecls = append(allDecls, clientDecls...)

	for _, decoy := range agDecoyTools {
		name, _ := decoy["name"].(string) // nolint:errcheck
		if !seenNames[name] {
			seenNames[name] = true

			allDecls = append(allDecls, decoy)
		}
	}

	request["tools"] = []any{
		map[string]any{
			"functionDeclarations": allDecls,
		},
	}

	if contents, okC := request["contents"].([]any); okC {
		cloakContentsFunctions(contents)
	}
}

func resolveAntigravityProjectID(body map[string]any, cred Credentials) string {
	if p, ok := body["project"].(string); ok && p != "" {
		return p
	}

	if cred.ProjectID != "" {
		return cred.ProjectID
	}

	if cred.ProviderSpecificData != nil {
		if pid, ok := cred.ProviderSpecificData["projectId"].(string); ok && pid != "" {
			return pid
		}
	}

	return formats.GenerateProjectID()
}

const (
	maxAntigravityOutputTokens = 64000
	defaultThinkingAGSignature = "e2E="
)

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

var imageDimRegex = regexp.MustCompile(`-(\d+)x(\d+)$`)

func parseImageConfig(model string) (map[string]any, string) {
	config := map[string]any{"aspectRatio": "1:1"}
	cleanModel := model
	match := imageDimRegex.FindStringSubmatch(model)
	if len(match) == 3 {
		cleanModel = imageDimRegex.ReplaceAllString(model, "")
		w, _ := strconv.Atoi(match[1])
		h, _ := strconv.Atoi(match[2])
		if w > 0 && h > 0 {
			if w <= 16 && h <= 16 {
				config["aspectRatio"] = fmt.Sprintf("%d:%d", w, h)
			} else {
				d := gcd(w, h)
				config["aspectRatio"] = fmt.Sprintf("%d:%d", w/d, h/d)
			}
		}
	}
	return config, cleanModel
}

func isAntigravityImageModel(model string) bool {
	lower := strings.ToLower(model)

	return strings.Contains(lower, "image") || strings.Contains(lower, "imagen")
}

func sanitizeClaudeSystemInstruction(req map[string]any) {
	sysInst, ok := req["systemInstruction"].(map[string]any)
	if !ok {
		return
	}

	parts, okParts := sysInst["parts"].([]any)
	if !okParts {
		return
	}

	oldText := "You are a Claude agent, built on Anthropic's Claude Agent SDK."

	for _, pRaw := range parts {
		if p, okP := pRaw.(map[string]any); okP {
			if txt, okT := p["text"].(string); okT && strings.Contains(txt, oldText) {
				p["text"] = strings.ReplaceAll(txt, oldText, "")
			}
		}
	}
}

func sanitizeSinglePart(p map[string]any) (map[string]any, bool, bool) {
	hasFuncResp := false
	if _, okFR := p["functionResponse"]; okFR {
		hasFuncResp = true
	}

	if _, okFC := p["functionCall"]; okFC {
		if sig, okSig := p["thoughtSignature"].(string); !okSig || sig == "" {
			p["thoughtSignature"] = defaultThinkingAGSignature
		}
	}

	if _, okTh := p["thought"]; okTh {
		if _, okFC := p["functionCall"]; !okFC {
			return nil, hasFuncResp, false
		}
	}

	return p, hasFuncResp, true
}

func cleanParts(parts []any) ([]any, bool) {
	var (
		filtered    []any
		hasFuncResp bool
	)

	for _, pRaw := range parts {
		p, okPart := pRaw.(map[string]any)
		if !okPart {
			continue
		}

		sanitized, fr, keep := sanitizeSinglePart(p)
		if fr {
			hasFuncResp = true
		}

		if keep {
			filtered = append(filtered, sanitized)
		}
	}

	return filtered, hasFuncResp
}

func fixAntigravityContents(req map[string]any) {
	contents, ok := req["contents"].([]any)
	if !ok {
		return
	}

	for _, cRaw := range contents {
		c, okC := cRaw.(map[string]any)
		if !okC {
			continue
		}

		parts, okP := c["parts"].([]any)
		if !okP {
			continue
		}

		filtered, hasFuncResp := cleanParts(parts)
		c["parts"] = filtered

		if hasFuncResp {
			c["role"] = "user"
		}
	}
}

func uuidFromSeed(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

var agIdeRequestIdRegex = regexp.MustCompile(`^agent/[^/]+/\d+/[^/]+/\d+$`)

func buildIdeRequestId(body, req map[string]any, cred Credentials, model, requestType string) string {
	if rid, ok := body["requestId"].(string); ok && agIdeRequestIdRegex.MatchString(rid) {
		return rid
	}
	sessID := ""
	if s, ok := req["sessionId"].(string); ok && s != "" {
		sessID = s
	} else if cred.ProviderSpecificData != nil {
		if email, ok := cred.ProviderSpecificData["email"].(string); ok && email != "" {
			sessID = email
		}
	}
	if sessID == "" {
		sessID = "anonymous"
	}

	convID := uuidFromSeed("antigravity:conversation:" + sessID)
	trajID := uuidFromSeed(fmt.Sprintf("antigravity:trajectory:%s:%s:%s", sessID, model, requestType))
	contentCount := 1
	if contents, ok := req["contents"].([]any); ok && len(contents) > 0 {
		contentCount = len(contents)
	}
	step := contentCount*2 - 1
	if step < 1 {
		step = 1
	}
	return fmt.Sprintf("agent/%s/%d/%s/%d", convID, time.Now().UnixMilli(), trajID, step)
}


func (e *AntigravityExecutor) transform(model string, body map[string]any, cred Credentials) map[string]any {
	var request map[string]any
	if req, ok := body["request"].(map[string]any); ok {
		request = req
	} else {
		request = body
	}

	e.stripBlacklisted(request)
	e.stripBlacklisted(body)
	project := resolveAntigravityProjectID(body, cred)

	if isAntigravityImageModel(model) {
		imageConfig, cleanModel := parseImageConfig(model)
		var contents []any
		srcContents, _ := request["contents"].([]any)
		if len(srcContents) == 0 {
			srcContents, _ = body["contents"].([]any)
		}
		for _, cRaw := range srcContents {
			if c, ok := cRaw.(map[string]any); ok {
				var textParts []any
				if parts, okP := c["parts"].([]any); okP {
					for _, pRaw := range parts {
						if p, okP2 := pRaw.(map[string]any); okP2 {
							if txt, okT := p["text"].(string); okT {
								textParts = append(textParts, map[string]any{"text": txt})
							}
						}
					}
				}
				if len(textParts) > 0 {
					role := "user"
					if r, okR := c["role"].(string); okR && r != "" {
						role = r
					}
					contents = append(contents, map[string]any{"role": role, "parts": textParts})
				}
			}
		}

		imgReq := map[string]any{
			"contents": contents,
			"generationConfig": map[string]any{
				"temperature":     1.0,
				"topP":            0.95,
				"topK":            40,
				"maxOutputTokens": 8192,
				"imageConfig":     imageConfig,
			},
		}
		if sess, ok := request["sessionId"].(string); ok && sess != "" {
			imgReq["sessionId"] = sess
		}

		return map[string]any{
			"project":     project,
			"model":       cleanModel,
			"userAgent":   "antigravity",
			"requestType": "image_gen",
			"requestId":   buildIdeRequestId(body, imgReq, cred, cleanModel, "image_gen"),
			"request":     imgReq,
		}
	}

	cloakAntigravityTools(request)
	sanitizeClaudeSystemInstruction(request)
	fixAntigravityContents(request)

	if genCfg, ok := request["generationConfig"].(map[string]any); ok {
		if maxTok, okTok := genCfg["maxOutputTokens"].(float64); okTok && maxTok > maxAntigravityOutputTokens {
			genCfg["maxOutputTokens"] = maxAntigravityOutputTokens
		}
	}

	requestType := "agent"

	out := map[string]any{
		"project":     project,
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": requestType,
		"request":     request,
		"requestId":   buildIdeRequestId(body, request, cred, model, requestType),
	}

	return out
}


func isSafeIdentChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '_' || r == '.' || r == ':' || r == '-'
}

func sanitizeIdentRune(r rune, isFirst bool) rune {
	if !isSafeIdentChar(r) {
		return '_'
	}

	if isFirst && r >= '0' && r <= '9' {
		return '_'
	}

	return r
}

func sanitizeFunctionName(name string) string {
	if name == "" {
		return "_unknown"
	}

	var b strings.Builder

	for i, r := range name {
		b.WriteRune(sanitizeIdentRune(r, i == 0))
	}

	s := b.String()
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "_" + s
	}

	if len(s) > 64 {
		s = s[:64]
	}

	return s
}

// Execute executes Antigravity requests.
func (e *AntigravityExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	wrapped := e.transform(model, m, cred)

	payload, err := json.Marshal(wrapped)
	if err != nil {
		return nil, err
	}

	action := "generateContent"
	if stream && !isAntigravityImageModel(model) {
		action = "streamGenerateContent?alt=sse"
	}

	url := "https://daily-cloudcode-pa.googleapis.com/v1internal:" + action

	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		if !strings.Contains(base, "generateContent") {
			url = base + ":" + action
		} else {
			url = base
		}
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	h.Set("User-Agent", "antigravity/ide/2.1.1 darwin/arm64")

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}

	return e.DoPOST(ctx, url, h, payload)
}
