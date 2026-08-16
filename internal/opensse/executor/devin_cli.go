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
		},
	})
	RegisterSpecialized("dv", &DevinCliExecutor{
		Base: Base{
			Provider: "devin-cli",
			BaseURL:  "devin://acp/stdio",
		},
	})
}

type DevinCliExecutor struct {
	Base
}

func resolveDevinBin() string {
	if bin := strings.TrimSpace(os.Getenv("CLI_DEVIN_BIN")); bin != "" {
		return bin
	}
	home, _ := os.UserHomeDir()
	isWin := runtime.GOOS == "windows"

	var candidates []string
	if isWin {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
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

func buildDevinPromptText(messages []any) string {
	var lines []string
	for _, mRaw := range messages {
		msg, ok := mRaw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "" {
			role = "user"
		}
		text := ""
		switch c := msg["content"].(type) {
		case string:
			text = c
		case []any:
			var parts []string
			for _, p := range c {
				if pm, ok := p.(map[string]any); ok {
					if t, _ := pm["type"].(string); t == "text" {
						parts = append(parts, fmt.Sprint(pm["text"]))
					} else if t == "tool_use" {
						inJson, _ := json.Marshal(pm["input"])
						parts = append(parts, fmt.Sprintf("\n[Tool call %v id=%v]\n%s\n", pm["name"], pm["id"], string(inJson)))
					} else if t == "tool_result" {
						parts = append(parts, fmt.Sprintf("\n[Tool result id=%v]\n%v\n", pm["tool_use_id"], pm["content"]))
					}
				}
			}
			text = strings.Join(parts, "")
		}

		if toolCalls, ok := msg["tool_calls"].([]any); ok && len(toolCalls) > 0 {
			var tcParts []string
			for _, tcRaw := range toolCalls {
				if tc, ok := tcRaw.(map[string]any); ok {
					fn, _ := tc["function"].(map[string]any)
					name := ""
					args := ""
					if fn != nil {
						name, _ = fn["name"].(string)
						args, _ = fn["arguments"].(string)
					}
					tcParts = append(tcParts, fmt.Sprintf("[Tool call %s id=%v]\n%s", name, tc["id"], args))
				}
			}
			text = strings.Join(append([]string{text}, tcParts...), "\n\n")
		}

		if strings.TrimSpace(text) == "" {
			continue
		}

		if role == "system" {
			lines = append(lines, "[System]\n"+text)
		} else if role == "assistant" {
			lines = append(lines, "[Assistant]\n"+text)
		} else if role == "tool" {
			lines = append(lines, fmt.Sprintf("[Tool result id=%v]\n%s", msg["tool_call_id"], text))
		} else {
			lines = append(lines, "[User]\n"+text)
		}
	}
	if len(lines) == 0 {
		return "(empty)"
	}
	return strings.Join(lines, "\n\n")
}

func (e *DevinCliExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	messages, _ := m["messages"].([]any)
	if len(messages) == 0 {
		messages, _ = m["input"].([]any)
	}
	promptText := buildDevinPromptText(messages)
	devinBin := resolveDevinBin()

	agentType := strings.TrimSpace(os.Getenv("CLI_DEVIN_AGENT_TYPE"))
	args := []string{"acp"}
	if agentType != "" {
		args = append(args, "--agent-type", agentType)
	}

	cmd := exec.CommandContext(ctx, devinBin, args...)
	permMode := os.Getenv("DEVIN_PERMISSION_MODE")
	if permMode == "" {
		permMode = "bypass"
	}
	cmd.Env = append(os.Environ(), "DEVIN_PERMISSION_MODE="+permMode)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
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
	b, _ := json.Marshal(msg)
	b = append(b, '\n')
	_, _ = w.Write(b)
}

func wrapDevinACPStream(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, model, cid string, created int64, promptText string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		var closeOnce sync.Once
		cleanup := func() {
			closeOnce.Do(func() {
				_ = stdin.Close()
				_ = stdout.Close()
				_ = pw.Close()
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				_ = cmd.Wait()
			})
		}
		defer cleanup()

		writeSSE := func(obj map[string]any) {
			b, _ := json.Marshal(obj)
			_, _ = pw.Write([]byte("data: "))
			_, _ = pw.Write(b)
			_, _ = pw.Write([]byte("\n\n"))
		}

		idCounter := 1
		sendRPC := func(method string, params any) int {
			id := idCounter
			idCounter++
			sendJSONRPC(stdin, method, params, id)
			return id
		}

		// Step 1: Send initialize
		sendRPC("initialize", map[string]any{
			"protocolVersion": "0.3",
			"clientInfo":      map[string]any{"name": "flamerouter", "version": "1.0"},
			"capabilities":    map[string]any{},
		})

		sc := bufio.NewScanner(stdout)
		var initDone, sessionCreated, roleEmitted bool
		var sessionID string
		var totalText string

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var msg map[string]any
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				continue
			}

			// initialize response
			if !initDone && msg["result"] != nil && msg["method"] == nil {
				initDone = true
				sendRPC("session/new", map[string]any{
					"cwd":        os.TempDir(),
					"mcpServers": []any{},
					"model":      model,
				})
				continue
			}

			// session/new response
			if initDone && !sessionCreated && msg["result"] != nil && msg["method"] == nil {
				if res, ok := msg["result"].(map[string]any); ok {
					sessionID, _ = res["sessionId"].(string)
				}
				if sessionID == "" {
					writeSSE(map[string]any{"error": map[string]any{"message": "session/new returned no sessionId", "type": "devin_cli_error"}})
					break
				}
				sessionCreated = true
				sendRPC("session/prompt", map[string]any{
					"sessionId": sessionID,
					"prompt":    []any{map[string]any{"type": "text", "text": promptText}},
				})
				continue
			}

			// auto-approve permission requests
			if meth, _ := msg["method"].(string); meth == "session/request_permission" {
				reqID, _ := msg["id"]
				params, _ := msg["params"].(map[string]any)
				options, _ := params["options"].([]any)
				optID := "allow"
				if len(options) > 0 {
					if first, ok := options[0].(map[string]any); ok {
						if idStr, ok := first["optionId"].(string); ok {
							optID = idStr
						}
					}
				}
				resp := map[string]any{
					"jsonrpc": "2.0",
					"id":      reqID,
					"result":  map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optID}},
				}
				b, _ := json.Marshal(resp)
				_, _ = stdin.Write(append(b, '\n'))
				continue
			}

			// agent stopped
			if meth, _ := msg["method"].(string); meth == "_cognition.ai/agent_stopped" || meth == "$/agent_stopped" {
				break
			}

			// streaming notifications
			if meth, _ := msg["method"].(string); meth == "session/update" || meth == "$/update" {
				params, _ := msg["params"].(map[string]any)
				update, _ := params["update"].(map[string]any)
				if update == nil {
					update = params
				}
				upType, _ := update["sessionUpdate"].(string)
				if upType == "" {
					upType, _ = update["type"].(string)
				}

				deltaText := ""
				if c, ok := update["content"].(string); ok {
					deltaText = c
				} else if cm, ok := update["content"].(map[string]any); ok {
					deltaText, _ = cm["text"].(string)
				} else if d, ok := update["delta"].(string); ok {
					deltaText = d
				}

				if upType == "agent_message_chunk" || upType == "message_delta" || upType == "text_delta" || upType == "content_delta" {
					if deltaText != "" {
						if !roleEmitted {
							writeSSE(map[string]any{
								"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
								"choices": []any{map[string]any{
									"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
								}},
							})
							roleEmitted = true
						}
						totalText += deltaText
						writeSSE(map[string]any{
							"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
							"choices": []any{map[string]any{
								"index": 0, "delta": map[string]any{"content": deltaText}, "finish_reason": nil,
							}},
						})
					}
				} else if upType == "message_stop" || upType == "stop" || upType == "done" {
					break
				}
			}

			if msg["error"] != nil {
				writeSSE(map[string]any{"error": map[string]any{"message": fmt.Sprintf("ACP error: %v", msg["error"]), "type": "devin_cli_error"}})
				break
			}
		}

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
		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()
	return pr
}

func collectDevinACPNonStreaming(cmd *exec.Cmd, stdin io.WriteCloser, stdout io.ReadCloser, model, cid string, created int64, promptText string) ([]byte, error) {
	defer func() {
		_ = stdin.Close()
		_ = stdout.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
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
	var initDone, sessionCreated bool
	var sessionID string
	var totalText string

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		if !initDone && msg["result"] != nil && msg["method"] == nil {
			initDone = true
			sendRPC("session/new", map[string]any{
				"cwd":        os.TempDir(),
				"mcpServers": []any{},
				"model":      model,
			})
			continue
		}

		if initDone && !sessionCreated && msg["result"] != nil && msg["method"] == nil {
			if res, ok := msg["result"].(map[string]any); ok {
				sessionID, _ = res["sessionId"].(string)
			}
			if sessionID == "" {
				return nil, fmt.Errorf("session/new returned no sessionId")
			}
			sessionCreated = true
			sendRPC("session/prompt", map[string]any{
				"sessionId": sessionID,
				"prompt":    []any{map[string]any{"type": "text", "text": promptText}},
			})
			continue
		}

		if meth, _ := msg["method"].(string); meth == "_cognition.ai/agent_stopped" || meth == "$/agent_stopped" {
			break
		}

		if meth, _ := msg["method"].(string); meth == "session/update" || meth == "$/update" {
			params, _ := msg["params"].(map[string]any)
			update, _ := params["update"].(map[string]any)
			if update == nil {
				update = params
			}
			upType, _ := update["sessionUpdate"].(string)
			if upType == "" {
				upType, _ = update["type"].(string)
			}

			deltaText := ""
			if c, ok := update["content"].(string); ok {
				deltaText = c
			} else if cm, ok := update["content"].(map[string]any); ok {
				deltaText, _ = cm["text"].(string)
			} else if d, ok := update["delta"].(string); ok {
				deltaText = d
			}

			if upType == "agent_message_chunk" || upType == "message_delta" || upType == "text_delta" || upType == "content_delta" {
				totalText += deltaText
			} else if upType == "message_stop" || upType == "stop" || upType == "done" {
				break
			}
		}

		if msg["error"] != nil {
			return nil, fmt.Errorf("Devin ACP error: %v", msg["error"])
		}
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
