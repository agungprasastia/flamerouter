package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
)

// Embeddings handles OpenAI embeddings requests.
func Embeddings(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, _ executor.Executor, fb *fallback.Fallback, usageSink UsageSink) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	if m["input"] == nil {
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

	payload, err := json.Marshal(m)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "failed to marshal payload")
		return err
	}

	res, err := postOpenAIPath(ctx, cred, "/embeddings", payload)
	if err != nil {
		// fallback chat-style executor
		ex := executor.GetExecutor(providerID)

		res, err = ex.Execute(ctx, cred, modelName, payload, false)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return err
		}
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResultRecordExactUsage(w, res, st, providerID, modelName, conn.ID, usageSink)
}
