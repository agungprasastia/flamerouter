package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterSpecialized("devin-cli", &DevinCliExecutor{
		Base: Base{
			Provider: "devin-cli",
			BaseURL:  "devin://acp/stdio",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("dv", &DevinCliExecutor{
		Base: Base{
			Provider: "devin-cli",
			BaseURL:  "devin://acp/stdio",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
}

// DevinCliExecutor executes LLM completions via local Devin CLI in ACP mode.
type DevinCliExecutor struct {
	Base
}

func resolveDevinBin() string {
	if bin := strings.TrimSpace(os.Getenv("CLI_DEVIN_BIN")); bin != "" {
		return bin
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}

	isWin := runtime.GOOS == "windows"

	var candidates []string

	if isWin {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" && home != "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}

		candidates = []string{
			filepath.Join(localAppData, "devin", "cli", "bin", "devin.exe"),
			filepath.Join(home, ".local", "bin", "devin.exe"),
			filepath.Join(home, "scoop", "shims", "devin.exe"),
			filepath.Join(localAppData, "Programs", "devin", "devin.exe"),
		}
	} else {
		candidates = []string{
			filepath.Join(home, ".local", "share", "devin", "bin", "devin"),
			filepath.Join(home, ".devin", "bin", "devin"),
			filepath.Join(home, ".local", "bin", "devin"),
			"/opt/homebrew/bin/devin",
			"/usr/local/bin/devin",
			"/usr/bin/devin",
		}
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}

	if isWin {
		return "devin.exe"
	}

	return "devin"
}

func formatDevinContentPart(p any) string {
	pm, ok := p.(map[string]any)
	if !ok {
		return ""
	}

	t, _ := pm["type"].(string) // nolint:errcheck
	switch t {
	case "text":
		return fmt.Sprint(pm["text"])
	case "tool_use":
		inJSON, err := json.Marshal(pm["input"])
		if err == nil {
			return fmt.Sprintf("\n[Tool call %v id=%v]\n%s\n", pm["name"], pm["id"], string(inJSON))
		}

		return ""
	case "tool_result":
		return fmt.Sprintf("\n[Tool result id=%v]\n%v\n", pm["tool_use_id"], pm["content"])
	default:
		return ""
	}
}

func formatDevinContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string

		for _, p := range c {
			if s := formatDevinContentPart(p); s != "" {
				parts = append(parts, s)
			}
		}

		return strings.Join(parts, "")
	default:
		return ""
	}
}

func formatDevinToolCalls(toolCalls []any) string {
	var tcParts []string

	for _, tcRaw := range toolCalls {
		if tc, ok := tcRaw.(map[string]any); ok {
			fn, _ := tc["function"].(map[string]any) // nolint:errcheck
			name := ""
			args := ""

			if fn != nil {
				name, _ = fn["name"].(string)      // nolint:errcheck
				args, _ = fn["arguments"].(string) // nolint:errcheck
			}

			tcParts = append(tcParts, fmt.Sprintf("[Tool call %s id=%v]\n%s", name, tc["id"], args))
		}
	}

	return strings.Join(tcParts, "\n\n")
}

func formatDevinMessageItem(msg map[string]any) (string, string) {
	role, _ := msg["role"].(string) // nolint:errcheck
	if role == "" {
		role = "user"
	}

	text := formatDevinContent(msg["content"])

	if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
		tcStr := formatDevinToolCalls(toolCalls)
		if text != "" && tcStr != "" {
			text = text + "\n\n" + tcStr
		} else if tcStr != "" {
			text = tcStr
		}
	}

	return role, text
}

func formatDevinRoleLine(role, text string, toolCallID any) string {
	switch role {
	case "system":
		return "[System]\n" + text
	case "assistant":
		return "[Assistant]\n" + text
	case "tool":
		return fmt.Sprintf("[Tool result id=%v]\n%s", toolCallID, text)
	default:
		return "[User]\n" + text
	}
}

func buildDevinPromptText(messages []any) string {
	lines := make([]string, 0, len(messages))

	for _, mRaw := range messages {
		msg, ok := mRaw.(map[string]any)
		if !ok {
			continue
		}

		role, text := formatDevinMessageItem(msg)
		if strings.TrimSpace(text) == "" {
			continue
		}

		lines = append(lines, formatDevinRoleLine(role, text, msg["tool_call_id"]))
	}

	if len(lines) == 0 {
		return "(empty)"
	}

	return strings.Join(lines, "\n\n")
}

func spawnDevinCmd(ctx context.Context, devinBin string) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {
	agentType := strings.TrimSpace(os.Getenv("CLI_DEVIN_AGENT_TYPE"))
	args := []string{"acp"}

	if agentType != "" {
		args = append(args, "--agent-type", agentType)
	}

	cmd := exec.CommandContext(ctx, devinBin, args...) // #nosec G204

	permMode := os.Getenv("DEVIN_PERMISSION_MODE")
	if permMode == "" {
		permMode = "bypass"
	}

	cmd.Env = append(os.Environ(), "DEVIN_PERMISSION_MODE="+permMode)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	if startErr := cmd.Start(); startErr != nil {
		return nil, nil, nil, startErr
	}

	return cmd, stdin, stdout, nil
}

// Execute runs a chat completion via local Devin CLI ACP protocol.
func (e *DevinCliExecutor) Execute(ctx context.Context, _ Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	messages, ok := m["messages"].([]any)
	if !ok || len(messages) == 0 {
		messages, _ = m["input"].([]any) // nolint:errcheck
	}

	promptText := buildDevinPromptText(messages)
	devinBin := resolveDevinBin()

	cmd, stdin, stdout, err := spawnDevinCmd(ctx, devinBin)
	if err != nil {
		return jsonErr(502, fmt.Sprintf("Devin CLI spawn error: %v", err), "devin_cli_error", "spawn_failed"), nil
	}

	cid := fmt.Sprintf("chatcmpl-devin-%d", time.Now().UnixMilli())
	created := time.Now().Unix()

	if stream {
		sseBody := wrapDevinACPStream(cmd, stdin, stdout, model, cid, created, promptText)

		return &Result{
			StatusCode: 200,
			Header: http.Header{
				"Content-Type":  []string{"text/event-stream"},
				"Cache-Control": []string{"no-cache"},
				"Connection":    []string{"keep-alive"},
			},
			Body: sseBody,
		}, nil
	}

	jsonResp, err := collectDevinACPNonStreaming(cmd, stdin, stdout, model, cid, created, promptText)
	if err != nil {
		return jsonErr(502, err.Error(), "devin_cli_error", ""), nil
	}

	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(jsonResp)),
	}, nil
}

func sendJSONRPC(w io.Writer, method string, params any, id int) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return
	}

	b = append(b, '\n')
	_, _ = w.Write(b) // nolint:errcheck
}

func extractDevinUpdateTypeAndText(params map[string]any) (string, string) {
	update, ok := params["update"].(map[string]any)
	if !ok || update == nil {
		update = params
	}

	upType, _ := update["sessionUpdate"].(string) // nolint:errcheck
	if upType == "" {
		upType, _ = update["type"].(string) // nolint:errcheck
	}

	deltaText := ""
	if c, ok := update["content"].(string); ok {
		deltaText = c
	} else if cm, ok := update["content"].(map[string]any); ok {
		deltaText, _ = cm["text"].(string) // nolint:errcheck
	} else if d, ok := update["delta"].(string); ok {
		deltaText = d
	}

	return upType, deltaText
}

func isDevinDeltaType(upType string) bool {
	return upType == "agent_message_chunk" || upType == "message_delta" || upType == "text_delta" || upType == "content_delta"
}

func emitDevinChunk(cid, model, content string, created int64, roleEmitted *bool, writeSSE func(map[string]any)) {
	if content == "" {
		return
	}

	if !*roleEmitted {
		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
			}},
		})

		*roleEmitted = true
	}

	writeSSE(map[string]any{
		"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil,
		}},
	})
}

func handleDevinSessionUpdate(params map[string]any, cid, model string, created int64, roleEmitted *bool, totalText *string, writeSSE func(map[string]any)) bool {
	upType, deltaText := extractDevinUpdateTypeAndText(params)

	if isDevinDeltaType(upType) {
		if deltaText != "" {
			*totalText += deltaText
			emitDevinChunk(cid, model, deltaText, created, roleEmitted, writeSSE)
		}

		return false
	}

	if upType == "agent_message" || upType == "message" {
		if deltaText != "" && *totalText == "" {
			*totalText = deltaText
			emitDevinChunk(cid, model, deltaText, created, roleEmitted, writeSSE)
		}

		return false
	}

	return upType == "turn_complete" || upType == "session_complete" || upType == "done"
}

func extractDevinPermissionOption(msg map[string]any) string {
	params, ok := msg["params"].(map[string]any)
	if !ok {
		return "allow"
	}

	options, ok := params["options"].([]any)
	if !ok || len(options) == 0 {
		return "allow"
	}

	first, ok := options[0].(map[string]any)
	if !ok {
		return "allow"
	}

	if idStr, ok := first["optionId"].(string); ok && idStr != "" {
		return idStr
	}

	return "allow"
}

func handleDevinPermissionRequest(msg map[string]any, stdin io.Writer) {
	reqID := msg["id"]
	optID := extractDevinPermissionOption(msg)

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"result":  map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optID}},
	}

	b, err := json.Marshal(resp)
	if err != nil {
		return
	}

	_, _ = stdin.Write(append(b, '\n')) // nolint:errcheck
}

func handleDevinLifecycle(msg map[string]any, initDone, sessionCreated *bool, sessionID *string, model, promptText string, sendRPC func(string, any) int, onError func(string)) (handled bool, shouldStop bool) {
	if !*initDone && msg["result"] != nil && msg["method"] == nil {
		*initDone = true

		sendRPC("session/new", map[string]any{
			"cwd":        os.TempDir(),
			"mcpServers": []any{},
			"model":      model,
		})

		return true, false
	}

	if *initDone && !*sessionCreated && msg["result"] != nil && msg["method"] == nil {
		if res, ok := msg["result"].(map[string]any); ok {
			*sessionID, _ = res["sessionId"].(string) // nolint:errcheck
		}

		if *sessionID == "" {
			onError("session/new returned no sessionId")

			return true, true
		}

		*sessionCreated = true

		sendRPC("session/prompt", map[string]any{
			"sessionId": *sessionID,
			"prompt":    []any{map[string]any{"type": "text", "text": promptText}},
		})

		return true, false
	}

	return false, false
}

func handleDevinStreamMsg(msg map[string]any, initDone, sessionCreated, roleEmitted *bool, sessionID, totalText *string, stdin io.WriteCloser, model, cid, promptText string, created int64, sendRPC func(string, any) int, writeSSE func(map[string]any)) bool {
	handled, stop := handleDevinLifecycle(msg, initDone, sessionCreated, sessionID, model, promptText, sendRPC, func(errStr string) {
		writeSSE(map[string]any{"error": map[string]any{"message": errStr, "type": "devin_cli_error"}})
	})
	if handled {
		return stop
	}

	meth, _ := msg["method"].(string) // nolint:errcheck
	switch meth {
	case "session/request_permission":
		handleDevinPermissionRequest(msg, stdin)
		return false
	case "_cognition.ai/agent_stopped", "$/agent_stopped":
		return true
	case "session/update", "$/update":
		params, ok := msg["params"].(map[string]any)
		if ok {
			return handleDevinSessionUpdate(params, cid, model, created, roleEmitted, totalText, writeSSE)
		}

		return false
	default:
		if msg["error"] != nil {
			writeSSE(map[string]any{"error": map[string]any{"message": fmt.Sprintf("ACP error: %v", msg["error"]), "type": "devin_cli_error"}})
			return true
		}

		return false
	}
}

func runDevinACPStreamLoop(sc *bufio.Scanner, stdin io.WriteCloser, model, cid, promptText string, created int64, sendRPC func(string, any) int, writeSSE func(map[string]any)) string {
	var (
		initDone, sessionCreated, roleEmitted bool
		sessionID, totalText                  string
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if handleDevinStreamMsg(msg, &initDone, &sessionCreated, &roleEmitted, &sessionID, &totalText, stdin, model, cid, promptText, created, sendRPC, writeSSE) {
			break
		}
	}

	return totalText
}

func wrapDevinACPStream(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, model, cid string, created int64, promptText string) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		var closeOnce sync.Once

		cleanup := func() {
			closeOnce.Do(func() {
				_ = stdin.Close()  // nolint:errcheck
				_ = stdout.Close() // nolint:errcheck
				_ = pw.Close()     // nolint:errcheck

				if cmd.Process != nil {
					_ = cmd.Process.Kill() // nolint:errcheck
				}

				_ = cmd.Wait() // nolint:errcheck
			})
		}
		defer cleanup()

		writeSSE := func(obj map[string]any) {
			b, err := json.Marshal(obj)
			if err != nil {
				return
			}

			_, _ = pw.Write([]byte("data: ")) // nolint:errcheck
			_, _ = pw.Write(b)                // nolint:errcheck
			_, _ = pw.Write([]byte("\n\n"))   // nolint:errcheck
		}

		idCounter := 1
		sendRPC := func(method string, params any) int {
			id := idCounter
			idCounter++

			sendJSONRPC(stdin, method, params, id)

			return id
		}

		sendRPC("initialize", map[string]any{
			"protocolVersion": "0.3",
			"clientInfo":      map[string]any{"name": "flamerouter", "version": "1.0"},
			"capabilities":    map[string]any{},
		})

		sc := bufio.NewScanner(stdout)
		totalText := runDevinACPStreamLoop(sc, stdin, model, cid, promptText, created, sendRPC, writeSSE)

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     (len(promptText) + 3) / 4,
				"completion_tokens": (len(totalText) + 3) / 4,
				"total_tokens":      (len(promptText) + len(totalText) + 3) / 4,
			},
		})

		_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
	}()

	return pr
}

func applyDevinNonStreamDelta(upType, deltaText string, totalText *string) bool {
	if isDevinDeltaType(upType) {
		*totalText += deltaText
		return false
	}

	if upType == "agent_message" || upType == "message" {
		if deltaText != "" && *totalText == "" {
			*totalText = deltaText
		}

		return false
	}

	return upType == "message_stop" || upType == "stop" || upType == "done" || upType == "turn_complete"
}

func processDevinACPNonStreamingUpdate(msg map[string]any, totalText *string) bool {
	meth, okMeth := msg["method"].(string)
	if okMeth && (meth == "_cognition.ai/agent_stopped" || meth == "$/agent_stopped") {
		return true
	}

	if okMeth && (meth == "session/update" || meth == "$/update") {
		params, _ := msg["params"].(map[string]any) // nolint:errcheck
		upType, deltaText := extractDevinUpdateTypeAndText(params)

		return applyDevinNonStreamDelta(upType, deltaText, totalText)
	}

	return false
}

func handleDevinNonStreamMsg(msg map[string]any, initDone, sessionCreated *bool, sessionID, totalText *string, stdin io.WriteCloser, model, promptText string, sendRPC func(string, any) int) (bool, error) {
	var errResult error

	handled, stop := handleDevinLifecycle(msg, initDone, sessionCreated, sessionID, model, promptText, sendRPC, func(errStr string) {
		errResult = fmt.Errorf("%s", errStr)
	})

	if handled {
		return stop, errResult
	}

	if meth, ok := msg["method"].(string); ok && meth == "session/request_permission" {
		handleDevinPermissionRequest(msg, stdin)
		return false, nil
	}

	if processDevinACPNonStreamingUpdate(msg, totalText) {
		return true, nil
	}

	if msg["error"] != nil {
		return true, fmt.Errorf("devin ACP error: %v", msg["error"])
	}

	return false, nil
}

func runDevinACPNonStreamLoop(sc *bufio.Scanner, stdin io.WriteCloser, model, promptText string, sendRPC func(string, any) int) (string, error) {
	var (
		initDone, sessionCreated bool
		sessionID, totalText     string
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		shouldBreak, err := handleDevinNonStreamMsg(msg, &initDone, &sessionCreated, &sessionID, &totalText, stdin, model, promptText, sendRPC)
		if err != nil {
			return "", err
		}

		if shouldBreak {
			break
		}
	}

	return totalText, nil
}

func collectDevinACPNonStreaming(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, model, cid string, created int64, promptText string) ([]byte, error) {
	defer func() {
		_ = stdin.Close()  // nolint:errcheck
		_ = stdout.Close() // nolint:errcheck

		if cmd.Process != nil {
			_ = cmd.Process.Kill() // nolint:errcheck
		}

		_ = cmd.Wait() // nolint:errcheck
	}()

	idCounter := 1
	sendRPC := func(method string, params any) int {
		id := idCounter
		idCounter++

		sendJSONRPC(stdin, method, params, id)

		return id
	}

	sendRPC("initialize", map[string]any{
		"protocolVersion": "0.3",
		"clientInfo":      map[string]any{"name": "flamerouter", "version": "1.0"},
		"capabilities":    map[string]any{},
	})

	sc := bufio.NewScanner(stdout)

	totalText, err := runDevinACPNonStreamLoop(sc, stdin, model, promptText, sendRPC)
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": totalText},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens":     (len(promptText) + 3) / 4,
			"completion_tokens": (len(totalText) + 3) / 4,
			"total_tokens":      (len(promptText) + len(totalText) + 3) / 4,
		},
	}

	return json.Marshal(out)
}
