package handlers

import (
	"bytes"
	"context"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

func resolveProviderConn(st *store.Store, fb *fallback.Fallback, modelStr string) (providerID, modelName string, conn *store.Connection, errMsg string) {
	mref := model.ParseModel(modelStr)
	if mref.Provider == "" {
		return "", "", nil, "model must be provider/model format"
	}

	providerID = model.ResolveProviderAlias(mref.Provider, nil)

	aliases, _ := st.ListAliases() //nolint:errcheck // optional alias list
	if mref.IsAlias {
		if resolved, ok := model.ResolveModelAlias(mref.Model, aliases); ok {
			mref = resolved
			providerID = model.ResolveProviderAlias(mref.Provider, nil)
		}
	}

	conn, _ = fb.SelectAccountExcluding(providerID, make(map[string]bool)) //nolint:errcheck // error handled by nil check
	if conn == nil {
		return providerID, mref.Model, nil, "no active connection for provider " + providerID
	}

	return providerID, mref.Model, conn, ""
}

func mediaCredentials(conn *store.Connection) executor.Credentials {
	if conn == nil {
		return executor.Credentials{
			ProviderSpecificData: nil,
			APIKey:               "",
			AccessToken:          "",
			RefreshToken:         "",
			BaseURL:              "",
			ProjectID:            "",
		}
	}

	return executor.Credentials{
		ProviderSpecificData: conn.ProviderSpecificData,
		APIKey:               conn.APIKey,
		AccessToken:          firstNonEmpty(conn.AccessToken, conn.APIKey),
		RefreshToken:         conn.RefreshToken,
		BaseURL:              conn.BaseURL,
		ProjectID:            "",
	}
}

// postOpenAIPath posts JSON to {base}/{path} with bearer auth (images/audio/embeddings).
func postOpenAIPath(ctx context.Context, cred executor.Credentials, path string, payload []byte) (*executor.Result, error) {
	url := resolveOpenAIEndpoint(cred.BaseURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close() //nolint:errcheck // best-effort body close
		}

		return nil, fmt.Errorf("nil response from upstream")
	}

	return &executor.Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}

func resolveOpenAIEndpoint(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}

	if strings.HasSuffix(base, path) {
		return base
	}

	switch {
	case strings.HasSuffix(base, "/v1"):
		return base + path
	case strings.Contains(base, "/v1/"):
		return base[:strings.Index(base, "/v1/")+3] + path
	default:
		return base + path
	}
}

// postMultipart posts multipart form (STT).
func postMultipart(ctx context.Context, cred executor.Credentials, path string, fields map[string]string, fileField, fileName string, fileData []byte, contentType string) (*executor.Result, error) {
	buf, formCT, err := buildMultipartBuffer(fields, fileField, fileName, fileData)
	if err != nil {
		return nil, err
	}

	url := resolveOpenAIEndpoint(cred.BaseURL, path)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, buf)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", formCT)

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	_ = contentType

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close() //nolint:errcheck // best-effort body close
		}

		return nil, fmt.Errorf("nil response from upstream")
	}

	return &executor.Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}

func buildMultipartBuffer(fields map[string]string, fileField, fileName string, fileData []byte) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer

	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v) //nolint:errcheck // buffer write
	}

	if fileData != nil {
		part, err := w.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, "", err
		}

		if _, err := part.Write(fileData); err != nil {
			return nil, "", err
		}
	}

	_ = w.Close() //nolint:errcheck // buffer close

	return &buf, w.FormDataContentType(), nil
}

func writeResult(w http.ResponseWriter, res *executor.Result) error {
	defer res.Body.Close() //nolint:errcheck // best-effort body close

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	ct := res.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}

	w.Header().Set("Content-Type", ct)
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(respBody) //nolint:errcheck // handler write

	return nil
}

func ensureModelField(m map[string]any, modelName string) {
	m["model"] = modelName
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, fmt.Sprintf(`{"error":%q}`, msg), status)
}
