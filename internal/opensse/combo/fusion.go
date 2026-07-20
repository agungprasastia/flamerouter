package combo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
)

// Fusion sends request to all models simultaneously, then merges/judges results.
type Fusion struct{}

type panelRes struct {
	model string
	text  string
	ok    bool
}

const (
	fusionMinPanel       = 2
	fusionStragglerGrace = 8 * time.Second
	fusionHardTimeout    = 90 * time.Second
)

type captureWriter struct {
	header http.Header
	code   int
	buf    bytes.Buffer
}

func (c *captureWriter) Header() http.Header {
	if c.header == nil {
		c.header = make(http.Header)
	}
	return c.header
}
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(statusCode int)   { c.code = statusCode }

func (f *Fusion) Execute(ctx context.Context, w http.ResponseWriter, body []byte,
	models []string, st *store.Store, exec executor.Executor,
	fb *fallback.Fallback, opts Options) error {

	if opts.SingleModel == nil {
		http.Error(w, `{"error":"combo single-model runner not configured"}`, http.StatusInternalServerError)
		return nil
	}
	panel := filterNonEmpty(models)
	if len(panel) == 0 {
		http.Error(w, `{"error":"Fusion combo has no models"}`, http.StatusBadRequest)
		return nil
	}
	if len(panel) == 1 {
		return opts.SingleModel(ctx, w, body, panel[0], opts.Stream)
	}

	// Panel: non-streaming, tools stripped
	panelBody, err := stripToolsForceNoStream(body)
	if err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	results := make([]panelRes, len(panel))
	var wg sync.WaitGroup
	minPanel := fusionMinPanel
	if minPanel > len(panel) {
		minPanel = len(panel)
	}
	done := make(chan struct{})
	var okCount int
	var mu sync.Mutex
	var graceOnce sync.Once

	ctxPanel, cancel := context.WithTimeout(ctx, fusionHardTimeout)
	defer cancel()

	for i, m := range panel {
		wg.Add(1)
		go func(i int, modelStr string) {
			defer wg.Done()
			cw := &captureWriter{}
			err := opts.SingleModel(ctxPanel, cw, panelBody, modelStr, false)
			text := extractPanelText(cw.buf.Bytes())
			ok := err == nil && cw.code < 400 && text != ""
			if cw.code == 0 && err == nil && text != "" {
				ok = true
			}
			results[i] = panelRes{model: modelStr, text: text, ok: ok}
			if ok {
				mu.Lock()
				okCount++
				n := okCount
				mu.Unlock()
				if n >= minPanel {
					graceOnce.Do(func() {
						go func() {
							select {
							case <-time.After(fusionStragglerGrace):
								cancel()
							case <-done:
							}
						}()
					})
				}
			}
		}(i, m)
	}
	wg.Wait()
	close(done)

	var answers []panelRes
	for _, r := range results {
		if r.ok && r.text != "" {
			answers = append(answers, r)
		}
	}
	if len(answers) == 0 {
		http.Error(w, `{"error":"All fusion panel models failed"}`, http.StatusServiceUnavailable)
		return nil
	}
	if len(answers) == 1 {
		return writePanelBrief(w, answers[0].text, opts.Stream)
	}

	judge := strings.TrimSpace(opts.JudgeModel)
	if judge == "" {
		// No judge configured: return first successful panel response as-is (no re-query, no judge call).
		return writePanelBrief(w, answers[0].text, opts.Stream)
	}
	judgeBody, err := appendJudgeTurn(body, buildJudgePrompt(answers))
	if err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}
	return opts.SingleModel(ctx, w, judgeBody, judge, opts.Stream)
}

// writePanelBrief emits a captured panel answer without another model round-trip.
func writePanelBrief(w http.ResponseWriter, text string, stream bool) error {
	if stream {
		if w.Header().Get("Content-Type") == "" {
			h := w.Header()
			h.Set("Content-Type", "text/event-stream")
			h.Set("Cache-Control", "no-cache")
			h.Set("Connection", "keep-alive")
		}
		chunk, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{
				"delta":         map[string]any{"content": text},
				"finish_reason": nil,
			}},
		})
		if _, err := w.Write([]byte("data: " + string(chunk) + "\n\n")); err != nil {
			return err
		}
		done, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
		})
		if _, err := w.Write([]byte("data: " + string(done) + "\n\n")); err != nil {
			return err
		}
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			return err
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return nil
	}
	resp, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(resp)
	return err
}

func filterNonEmpty(models []string) []string {
	out := make([]string, 0, len(models))
	for _, m := range models {
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

func stripToolsForceNoStream(body []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	delete(m, "tools")
	delete(m, "tool_choice")
	m["stream"] = false
	return json.Marshal(m)
}

func buildJudgePrompt(answers []panelRes) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the JUDGE in a model-fusion panel. %d expert models independently answered the user's most recent request. Their responses are below, anonymized by source.\n\n", len(answers))
	b.WriteString("Do NOT mention that multiple models were used, and do NOT refer to the sources. Produce ONE authoritative final answer addressed directly to the user.\n\n")
	b.WriteString("First, internally analyze the panel along these dimensions: consensus (points most sources agree on - treat as higher-confidence), contradictions (where they disagree - resolve with your own judgment), partial coverage, unique insights only one source surfaced, and blind spots every source missed. Then write the best possible final answer grounded in that analysis - more complete and correct than any single response, with no filler.\n\n")
	b.WriteString("=== PANEL RESPONSES ===\n")
	for i, a := range answers {
		fmt.Fprintf(&b, "[Source %d]\n%s\n\n", i+1, a.text)
	}
	b.WriteString("=== END PANEL RESPONSES ===\n\n")
	b.WriteString("Now write the final answer to the user's original request.")
	return b.String()
}

func appendJudgeTurn(body []byte, text string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if msgs, ok := m["messages"].([]any); ok {
		m["messages"] = append(msgs, map[string]any{"role": "user", "content": text})
	} else if input, ok := m["input"].([]any); ok {
		m["input"] = append(input, map[string]any{"role": "user", "content": text})
	} else if contents, ok := m["contents"].([]any); ok {
		m["contents"] = append(contents, map[string]any{"role": "user", "parts": []any{map[string]any{"text": text}}})
	} else {
		m["messages"] = []any{map[string]any{"role": "user", "content": text}}
	}
	return json.Marshal(m)
}

func extractPanelText(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	// strip SSE if present
	if bytes.HasPrefix(raw, []byte("data:")) {
		var texts []string
		for _, line := range bytes.Split(raw, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if bytes.Equal(data, []byte("[DONE]")) {
				break
			}
			var chunk map[string]any
			if json.Unmarshal(data, &chunk) != nil {
				continue
			}
			if t := textFromJSON(chunk); t != "" {
				texts = append(texts, t)
			}
		}
		return strings.Join(texts, "")
	}
	var j map[string]any
	if json.Unmarshal(raw, &j) != nil {
		return string(raw)
	}
	return textFromJSON(j)
}

func textFromJSON(jsonMap map[string]any) string {
	if choices, ok := jsonMap["choices"].([]any); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]any); ok {
			if msg, ok := c0["message"].(map[string]any); ok {
				if t := contentToString(msg["content"]); t != "" {
					return t
				}
			}
			if d, ok := c0["delta"].(map[string]any); ok {
				if t := contentToString(d["content"]); t != "" {
					return t
				}
			}
			if t, ok := c0["text"].(string); ok {
				return t
			}
		}
	}
	if t := contentToString(jsonMap["content"]); t != "" {
		return t
	}
	if cands, ok := jsonMap["candidates"].([]any); ok && len(cands) > 0 {
		if c0, ok := cands[0].(map[string]any); ok {
			if content, ok := c0["content"].(map[string]any); ok {
				if parts, ok := content["parts"].([]any); ok {
					var b strings.Builder
					for _, p := range parts {
						if pm, ok := p.(map[string]any); ok {
							if t, ok := pm["text"].(string); ok {
								b.WriteString(t)
							}
						}
					}
					return b.String()
				}
			}
		}
	}
	return ""
}

func contentToString(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	default:
		return ""
	}
}

