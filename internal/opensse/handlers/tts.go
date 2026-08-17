package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
)

// TTS handles OpenAI text-to-speech requests.
func TTS(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, _ executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	input, _ := m["input"].(string) //nolint:errcheck // optional type assertion
	if input == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: input")
		return nil
	}

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" || conn == nil {
		if errMsg == "" {
			errMsg = "connection not found"
		}

		jsonError(w, http.StatusBadRequest, errMsg)

		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	ensureModelField(m, modelName)

	if _, ok := m["voice"]; !ok {
		m["voice"] = "alloy"
	}

	payload, _ := json.Marshal(m) //nolint:errcheck // safe internal map marshal

	res, err := postOpenAIPath(ctx, cred, "/audio/speech", payload)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	defer res.Body.Close()              //nolint:errcheck // best-effort body close
	respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read

	if res.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(respBody) //nolint:errcheck // handler write

		return nil
	}

	fb.ClearError(conn.ID)

	ct := res.Header.Get("Content-Type")
	if ct == "" {
		ct = "audio/mpeg"
	}

	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody) //nolint:errcheck // handler write

	return nil
}

// STT handles speech-to-text audio transcriptions.
func STT(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	_ = exec

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "STT requires multipart form (file + model) or JSON; use multipart via gateway")
		return err
	}

	modelStr, _ := m["model"].(string) //nolint:errcheck // optional type assertion

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" || conn == nil {
		if errMsg == "" {
			errMsg = "connection not found"
		}

		jsonError(w, http.StatusBadRequest, errMsg)

		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	fields := extractSTTFields(m, modelName)
	fileData := extractSTTFileData(m)

	if fileData == nil {
		return forwardSTTJSON(ctx, w, cred, conn, modelName, m, fb)
	}

	return forwardSTTMultipart(ctx, w, cred, conn, fields, fileData, fb)
}

func extractSTTFields(m map[string]any, modelName string) map[string]string {
	fields := map[string]string{"model": modelName}

	for _, k := range []string{"language", "prompt", "response_format"} {
		if val, ok := m[k].(string); ok && val != "" {
			fields[k] = val
		}
	}

	return fields
}

func extractSTTFileData(m map[string]any) []byte {
	if b64, ok := m["file_base64"].(string); ok && b64 != "" {
		return []byte(b64)
	}

	return nil
}

func forwardSTTJSON(ctx context.Context, w http.ResponseWriter, cred executor.Credentials, conn *store.Connection, modelName string, m map[string]any, fb *fallback.Fallback) error {
	ensureModelField(m, modelName)
	payload, _ := json.Marshal(m) //nolint:errcheck // safe internal map marshal

	res, err := postOpenAIPath(ctx, cred, "/audio/transcriptions", payload)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res)
}

func forwardSTTMultipart(ctx context.Context, w http.ResponseWriter, cred executor.Credentials, conn *store.Connection, fields map[string]string, fileData []byte, fb *fallback.Fallback) error {
	res, err := postMultipart(ctx, cred, "/audio/transcriptions", fields, "file", "audio.webm", fileData, "")
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res)
}

// STTMultipart handles real multipart HTTP request (gateway should call this when Content-Type is multipart).
func STTMultipart(ctx context.Context, w http.ResponseWriter, r *http.Request, st *store.Store, fb *fallback.Fallback) error {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid multipart form")
		return err
	}

	modelStr := r.FormValue("model")

	providerID, modelName, conn, errMsg := resolveProviderConn(st, fb, modelStr)
	if errMsg != "" || conn == nil {
		if errMsg == "" {
			errMsg = "connection not found"
		}

		jsonError(w, http.StatusBadRequest, errMsg)

		return nil
	}

	_ = providerID
	cred := mediaCredentials(conn)

	fileName, fileData, err := extractMultipartFile(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return err
	}

	fields := extractMultipartFormFields(r, modelName)

	res, err := postMultipart(ctx, cred, "/audio/transcriptions", fields, "file", fileName, fileData, r.Header.Get("Content-Type"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res)
}

func extractMultipartFile(r *http.Request) (string, []byte, error) {
	file, hdr, err := r.FormFile("file")
	if err != nil {
		return "", nil, fmt.Errorf("missing file field")
	}

	defer file.Close() //nolint:errcheck // best-effort file close

	fileData, err := io.ReadAll(file)
	if err != nil {
		return "", nil, err
	}

	return hdr.Filename, fileData, nil
}

func extractMultipartFormFields(r *http.Request, modelName string) map[string]string {
	fields := map[string]string{"model": modelName}

	for _, k := range []string{"language", "prompt", "response_format", "temperature"} {
		if v := r.FormValue(k); v != "" {
			fields[k] = v
		}
	}

	return fields
}

// Voices returns supported voice list.
func Voices(_ context.Context, w http.ResponseWriter, _ *store.Store) error {
	w.Header().Set("Content-Type", "application/json")
	// Common OpenAI + common TTS voices list
	_, err := w.Write([]byte(`{"voices":[
		{"id":"alloy","name":"Alloy"},{"id":"echo","name":"Echo"},{"id":"fable","name":"Fable"},
		{"id":"onyx","name":"Onyx"},{"id":"nova","name":"Nova"},{"id":"shimmer","name":"Shimmer"}
	]}`))

	return err
}
