package combo

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	fusionHardTimeout    = 30 * time.Second
	fusionStragglerGrace = 1500 * time.Millisecond
	fusionMinPanel       = 2
)

// Fusion runs multiple models concurrently and synthesizes their answers with a judge.
type Fusion struct{}

type panelRes struct {
	model string
	text  string
	ok    bool
}

type captureWriter struct {
	header http.Header
	buf    bytes.Buffer
	code   int
}

// Header returns the HTTP header map.
func (c *captureWriter) Header() http.Header { return c.header }

// Write captures response bytes into an internal buffer.
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }

// WriteHeader captures the HTTP status code.
func (c *captureWriter) WriteHeader(statusCode int) { c.code = statusCode }

// Execute runs the combo using fusion strategy.
func (f *Fusion) Execute(ctx context.Context, w http.ResponseWriter, body []byte,
	models []string, _ *store.Store, _ executor.Executor,
	_ *fallback.Fallback, opts Options,
) error {
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

	panelBody, err := stripToolsForceNoStream(body)
	if err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	answers := executePanel(ctx, panel, panelBody, opts)
	if len(answers) == 0 {
		http.Error(w, `{"error":"All fusion panel models failed"}`, http.StatusServiceUnavailable)
		return nil
	}

	if len(answers) == 1 {
		return writePanelBrief(w, answers[0].text, opts.Stream)
	}

	judge := strings.TrimSpace(opts.JudgeModel)
	if judge == "" {
		return writePanelBrief(w, answers[0].text, opts.Stream)
	}

	judgeBody, err := appendJudgeTurn(body, buildJudgePrompt(answers))
	if err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return err
	}

	return opts.SingleModel(ctx, w, judgeBody, judge, opts.Stream)
}

func spawnPanelWorker(ctx context.Context, idx int, modelStr string, panelBody []byte, opts Options, wg *sync.WaitGroup, results []panelRes, onOk func()) {
	go func() {
		defer wg.Done()

		cw := &captureWriter{header: make(http.Header), buf: bytes.Buffer{}, code: 0}
		callErr := opts.SingleModel(ctx, cw, panelBody, modelStr, false)
		text := extractPanelText(cw.buf.Bytes())
		ok := (callErr == nil && cw.code < 400 && text != "") || (cw.code == 0 && callErr == nil && text != "")

		results[idx] = panelRes{model: modelStr, text: text, ok: ok}

		if ok {
			onOk()
		}
	}()
}

func executePanel(ctx context.Context, panel []string, panelBody []byte, opts Options) []panelRes {
	results := make([]panelRes, len(panel))
	minPanel := min(fusionMinPanel, len(panel))
	done := make(chan struct{})

	var (
		wg        sync.WaitGroup
		okCount   int
		mu        sync.Mutex
		graceOnce sync.Once
	)

	ctxPanel, cancel := context.WithTimeout(ctx, fusionHardTimeout)
	defer cancel()

	onOk := func() {
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

	for i, m := range panel {
		wg.Add(1)
		spawnPanelWorker(ctxPanel, i, m, panelBody, opts, &wg, results, onOk)
	}

	wg.Wait()
	close(done)

	var answers []panelRes

	for _, r := range results {
		if r.ok && r.text != "" {
			answers = append(answers, r)
		}
	}

	return answers
}

func writePanelBrief(w http.ResponseWriter, text string, stream bool) error {
	if stream {
		return writePanelStream(w, text)
	}

	return writePanelNonStream(w, text)
}

func writePanelStream(w http.ResponseWriter, text string) error {
	if w.Header().Get("Content-Type") == "" {
		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
	}

	chunk, errMarshal := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta":         map[string]any{"content": text},
			"finish_reason": nil,
		}},
	})
	if errMarshal != nil {
		return errMarshal
	}

	if _, errWrite := w.Write([]byte("data: " + string(chunk) + "\n\n")); errWrite != nil {
		return errWrite
	}

	done, errDone := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
	})
	if errDone != nil {
		return errDone
	}

	if _, errWriteDone := w.Write([]byte("data: " + string(done) + "\n\n")); errWriteDone != nil {
		return errWriteDone
	}

	if _, errDoneSig := w.Write([]byte("data: [DONE]\n\n")); errDoneSig != nil {
		return errDoneSig
	}

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	return nil
}

func writePanelNonStream(w http.ResponseWriter, text string) error {
	resp, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"role": "assistant", "content": text},
			"finish_reason": "stop",
		}},
	})
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")

	_, err = w.Write(resp)

	return err
}

func filterNonEmpty(in []string) []string {
	var out []string

	for _, s := range in {
		if s != "" {
			out = append(out, s)
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
	delete(m, "functions")
	delete(m, "function_call")
	m["stream"] = false

	return json.Marshal(m)
}

func appendJudgeTurn(body []byte, judgePrompt string) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	judgeMsg := map[string]any{
		"role":    "user",
		"content": judgePrompt,
	}

	if arr, ok := m["messages"].([]any); ok {
		m["messages"] = append(arr, judgeMsg)
	} else {
		m["messages"] = []any{judgeMsg}
	}

	return json.Marshal(m)
}

func buildJudgePrompt(answers []panelRes) string {
	var b strings.Builder

	b.WriteString("You are a synthesis judge. Below are draft responses from multiple AI models to the above prompt.\n")
	b.WriteString("Synthesize the best, most complete, accurate, and concise final response.\n\n")

	for i, a := range answers {
		b.WriteString("### Response ")
		b.WriteString(string(rune('A' + i)))
		b.WriteString(" (from ")
		b.WriteString(a.model)
		b.WriteString("):\n")
		b.WriteString(a.text)
		b.WriteString("\n\n")
	}

	b.WriteString("Final Answer:")

	return b.String()
}

func extractPanelText(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}

	if bytes.HasPrefix(raw, []byte("data:")) {
		return extractSSEText(raw)
	}

	var j map[string]any
	if json.Unmarshal(raw, &j) != nil {
		return string(raw)
	}

	return textFromJSON(j)
}

func extractSSEText(raw []byte) string {
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

func textFromChoices(choices []any) string {
	if len(choices) == 0 {
		return ""
	}

	c0, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}

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

	return ""
}

func textFromCandidates(cands []any) string {
	if len(cands) == 0 {
		return ""
	}

	c0, ok := cands[0].(map[string]any)
	if !ok {
		return ""
	}

	content, ok := c0["content"].(map[string]any)
	if !ok {
		return ""
	}

	parts, ok := content["parts"].([]any)
	if !ok {
		return ""
	}

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

func textFromJSON(jsonMap map[string]any) string {
	if choices, ok := jsonMap["choices"].([]any); ok {
		if t := textFromChoices(choices); t != "" {
			return t
		}
	}

	if t := contentToString(jsonMap["content"]); t != "" {
		return t
	}

	if cands, ok := jsonMap["candidates"].([]any); ok {
		if t := textFromCandidates(cands); t != "" {
			return t
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

		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
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
