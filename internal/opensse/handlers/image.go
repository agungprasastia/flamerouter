package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
)

func ImageGeneration(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid json")
		return err
	}

	prompt, _ := m["prompt"].(string)
	if prompt == "" {
		jsonError(w, http.StatusBadRequest, "missing required field: prompt")
		return nil
	}

	modelStr, _ := m["model"].(string)

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
	payload, _ := json.Marshal(m)

	// Prefer images generations path (OpenAI-compatible)
	res, err := postOpenAIPath(ctx, cred, "/images/generations", payload)
	if err != nil {
		// fallback to generic executor chat path
		res, err = executor.GetExecutor(providerID).Execute(ctx, cred, modelName, payload, false)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return err
		}
	}

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	return writeResult(w, res, true)
}
