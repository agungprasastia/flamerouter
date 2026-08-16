package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"io"
	"net/http"
)

func TTS(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	input, _ := m["input"].(string)
	if input == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: input")
		return nil
	}

	modelStr, _ := m["model"].(string)

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" {
		jsonError(w, http.StatusBadRequest, errMsg)
		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	ensureModelField(m, modelName)

	if _, ok := m["voice"]; !ok {
		m["voice"] = "alloy"
	}

	payload, _ := json.Marshal(m)

	res, err := postOpenAIPath(ctx, cred, "/audio/speech", payload)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	defer res.Body.Close()
	respBody, _ := io.ReadAll(res.Body)

	if res.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(respBody)

		return nil
	}

	fb.ClearError(conn.ID)

	ct := res.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}

	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)

	return nil
}

func STT(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	// Prefer multipart from request body when Content-Type is multipart —
	// gateway may pass raw body; try JSON first then fail clearly.
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		// not JSON — treat as opaque; still need model
		jsonError(w, http.StatusBadRequest, "STT requires multipart form (file + model) or JSON; use multipart via gateway")
		return err
	}

	modelStr, _ := m["model"].(string)

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" {
		jsonError(w, http.StatusBadRequest, errMsg)
		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	// If file is base64 in JSON (some clients)
	fields := map[string]string{"model": modelName}
	if lang, ok := m["language"].(string); ok && lang != "" {
		fields["language"] = lang
	}

	if prompt, ok := m["prompt"].(string); ok && prompt != "" {
		fields["prompt"] = prompt
	}

	if rf, ok := m["response_format"].(string); ok && rf != "" {
		fields["response_format"] = rf
	}

	var fileData []byte

	fileName := "audio.webm"

	if b64, ok := m["file_base64"].(string); ok && b64 != "" {
		// raw base64 without data: prefix
		fileData = []byte(b64) // caller should send real bytes via multipart gateway path
	}

	// If no file, forward as JSON to transcriptions (some providers accept URL)
	if fileData == nil {
		ensureModelField(m, modelName)
		payload, _ := json.Marshal(m)

		res, err := postOpenAIPath(ctx, cred, "/audio/transcriptions", payload)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return err
		}

		if res.StatusCode < 400 {
			fb.ClearError(conn.ID)
		}

		return writeResult(w, res, true)
	}

	res, err := postMultipart(ctx, cred, "/audio/transcriptions", fields, "file", fileName, fileData, "")
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res, true)
}

// STTMultipart handles real multipart HTTP request (gateway should call this when Content-Type is multipart).
func STTMultipart(ctx context.Context, w http.ResponseWriter, r *http.Request, st *store.Store, fb *fallback.Fallback) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid multipart form")
		return err
	}

	modelStr := r.FormValue("model")

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" {
		jsonError(w, http.StatusBadRequest, errMsg)
		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	file, hdr, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "missing file field")
		return err
	}

	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	fields := map[string]string{"model": modelName}

	for _, k := range []string{"language", "prompt", "response_format", "temperature"} {
		if v := r.FormValue(k); v != "" {
			fields[k] = v
		}
	}

	res, err := postMultipart(ctx, cred, "/audio/transcriptions", fields, "file", hdr.Filename, fileData, hdr.Header.Get("Content-Type"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res, true)
}

func Voices(ctx context.Context, w http.ResponseWriter, st *store.Store) error {
	w.Header().Set("Content-Type", "application/json")
	// Common OpenAI + common TTS voices list
	_, _ = w.Write([]byte(`{"voices":[
		{"id":"alloy","name":"Alloy"},{"id":"echo","name":"Echo"},{"id":"fable","name":"Fable"},
		{"id":"onyx","name":"Onyx"},{"id":"nova","name":"Nova"},{"id":"shimmer","name":"Shimmer"}
	]}`))

	return nil
}
