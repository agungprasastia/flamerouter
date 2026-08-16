package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	cfRelayWorker = `
export default {
  async fetch(request, env, ctx) {
    const target = request.headers.get("x-relay-target");
    const relayPath = request.headers.get("x-relay-path") || "/";
    
    if (!target) {
      return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), {
        status: 400,
        headers: { "content-type": "application/json" },
      });
    }

    const targetUrl = target.replace(/\/$/, "") + relayPath;
    const newRequestInit = {
      method: request.method,
      headers: new Headers(request.headers),
    };

    if (request.method !== "GET" && request.method !== "HEAD") {
      newRequestInit.body = request.body;
      newRequestInit.duplex = "half";
    }

    newRequestInit.headers.delete("x-relay-target");
    newRequestInit.headers.delete("x-relay-path");
    newRequestInit.headers.delete("host");

    try {
      const response = await fetch(targetUrl, newRequestInit);
      return new Response(response.body, {
        status: response.status,
        headers: response.headers,
      });
    } catch (error) {
      return new Response(JSON.stringify({ error: error.message }), {
        status: 502,
        headers: { "content-type": "application/json" },
      });
    }
  },
};
`

	denoRelayCode = `Deno.serve(async (request) => {
  const target = request.headers.get("x-relay-target");
  const relayPath = request.headers.get("x-relay-path") || "/";

  if (!target) {
    return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), {
      status: 400,
      headers: { "content-type": "application/json" },
    });
  }

  const targetUrl = target.replace(/\/$/, "") + relayPath;
  const newHeaders = new Headers(request.headers);
  newHeaders.delete("x-relay-target");
  newHeaders.delete("x-relay-path");
  newHeaders.delete("host");

  const init = {
    method: request.method,
    headers: newHeaders,
  };

  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = request.body;
    init.duplex = "half";
  }

  try {
    const response = await fetch(targetUrl, init);
    return new Response(response.body, {
      status: response.status,
      headers: response.headers,
    });
  } catch (error) {
    return new Response(JSON.stringify({ error: error.message }), {
      status: 502,
      headers: { "content-type": "application/json" },
    });
  }
});`

	vercelRelayCode = `
export const config = { runtime: "edge" };

export default async function handler(req) {
  const target = req.headers.get("x-relay-target");
  const relayPath = req.headers.get("x-relay-path") || "/";
  if (!target) {
    return new Response(JSON.stringify({ error: "Missing x-relay-target header" }), {
      status: 400,
      headers: { "content-type": "application/json" },
    });
  }

  const targetUrl = target.replace(/\/$/, "") + relayPath;

  const headers = new Headers(req.headers);
  headers.delete("x-relay-target");
  headers.delete("x-relay-path");
  headers.delete("host");

  const response = await fetch(targetUrl, {
    method: req.method,
    headers,
    body: req.method !== "GET" && req.method !== "HEAD" ? req.body : undefined,
    duplex: "half",
  });

  return new Response(response.body, {
    status: response.status,
    headers: response.headers,
  });
}
`
)

func defaultRelayName() string {
	return "relay-" + strconv.FormatInt(time.Now().UnixMilli(), 36)
}

func (s *Server) storeRelayPool(name, typ, deployURL string) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(deployURL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid deploy url")
	}

	host := u.Hostname()

	port := 443
	if u.Port() != "" {
		port, _ = strconv.Atoi(u.Port())
	} else if u.Scheme == "http" {
		port = 80
	}

	id, err := s.st.CreateProxyPool(name, typ, host, port, "", "")
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"id": id, "name": name, "type": typ,
		"proxyUrl": deployURL, "host": host, "port": port,
		"isActive": true, "noProxy": "", "strictProxy": false,
	}, nil
}

// POST /api/proxy-pools/cloudflare-deploy.
func (s *Server) handleCloudflareDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID   string `json:"accountId"`
		APIToken    string `json:"apiToken"`
		ProjectName string `json:"projectName"`
		// dry: return script only (no live deploy)
		DryRun bool `json:"dryRun"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	req.AccountID = strings.TrimSpace(req.AccountID)
	req.APIToken = strings.TrimSpace(req.APIToken)
	req.ProjectName = strings.TrimSpace(req.ProjectName)

	if req.ProjectName == "" {
		req.ProjectName = defaultRelayName()
	}

	if req.DryRun || req.APIToken == "" {
		if req.AccountID == "" || req.APIToken == "" {
			// still require fields for live; dry may omit token and get script
			if !req.DryRun {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Cloudflare Account ID and API Token are required"})
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"script":       strings.TrimSpace(cfRelayWorker),
			"projectName":  req.ProjectName,
			"instructions": "PUT workers/scripts via Cloudflare API with multipart metadata+index.js, then enable subdomain.",
		})

		return
	}

	if req.AccountID == "" || req.APIToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Cloudflare Account ID and API Token are required"})
		return
	}

	workerURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", req.AccountID, req.ProjectName)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("index.js", "index.js")
	_, _ = io.WriteString(part, strings.TrimSpace(cfRelayWorker)+"\n")
	metaPart, _ := mw.CreateFormFile("metadata", "metadata.json")
	meta, _ := json.Marshal(map[string]any{
		"main_module":        "index.js",
		"compatibility_date": "2024-03-20",
		"observability":      map[string]any{"enabled": true},
	})
	_, _ = metaPart.Write(meta)
	_ = mw.Close()

	uploadReq, err := http.NewRequestWithContext(r.Context(), http.MethodPut, workerURL, &buf)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	uploadReq.Header.Set("Authorization", "Bearer "+req.APIToken)
	uploadReq.Header.Set("Content-Type", mw.FormDataContentType())

	uploadRes, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	defer uploadRes.Body.Close()

	if uploadRes.StatusCode < 200 || uploadRes.StatusCode >= 300 {
		var errBody map[string]any
		_ = json.NewDecoder(uploadRes.Body).Decode(&errBody)
		msg := "Failed to upload Worker to Cloudflare"

		if errs, ok := errBody["errors"].([]any); ok && len(errs) > 0 {
			if e0, ok := errs[0].(map[string]any); ok {
				if m, ok := e0["message"].(string); ok && m != "" {
					msg = m
				}
			}
		}

		writeJSON(w, uploadRes.StatusCode, map[string]any{"error": msg})

		return
	}

	// enable workers.dev subdomain (best-effort)
	subReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, workerURL+"/subdomain", strings.NewReader(`{"enabled":true}`))
	subReq.Header.Set("Authorization", "Bearer "+req.APIToken)
	subReq.Header.Set("Content-Type", "application/json")

	if res, err := http.DefaultClient.Do(subReq); err == nil {
		res.Body.Close()
	}

	deployURL := ""
	acctSubReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
		fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/subdomain", req.AccountID), nil)
	acctSubReq.Header.Set("Authorization", "Bearer "+req.APIToken)

	if res, err := http.DefaultClient.Do(acctSubReq); err == nil {
		defer res.Body.Close()

		var data struct {
			Result struct {
				Subdomain string `json:"subdomain"`
			} `json:"result"`
		}

		_ = json.NewDecoder(res.Body).Decode(&data)

		if data.Result.Subdomain != "" {
			deployURL = fmt.Sprintf("https://%s.%s.workers.dev", req.ProjectName, data.Result.Subdomain)
		}
	}

	if deployURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "Worker deployed but failed to retrieve workers.dev subdomain. Make sure you have setup a workers.dev subdomain in Cloudflare Dashboard.",
		})

		return
	}

	pool, err := s.storeRelayPool(req.ProjectName, "cloudflare", deployURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}

// POST /api/proxy-pools/deno-deploy.
func (s *Server) handleDenoDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DenoToken   string `json:"denoToken"`
		OrgDomain   string `json:"orgDomain"`
		ProjectName string `json:"projectName"`
		DryRun      bool   `json:"dryRun"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	req.DenoToken = strings.TrimSpace(req.DenoToken)
	req.OrgDomain = strings.TrimSpace(req.OrgDomain)
	req.ProjectName = strings.TrimSpace(req.ProjectName)

	if req.ProjectName == "" {
		req.ProjectName = defaultRelayName()
	}

	if req.DryRun {
		writeJSONOK(w, map[string]any{
			"script": strings.TrimSpace(denoRelayCode), "projectName": req.ProjectName,
			"instructions": "Create Deno Deploy app then POST /apps/{id}/deploy with main.ts asset.",
		})

		return
	}

	if req.OrgDomain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Organization domain is required"})
		return
	}

	if req.DenoToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Deno Deploy API token is required"})
		return
	}

	const denoAPI = "https://api.deno.com/v2"

	createBody, _ := json.Marshal(map[string]any{
		"slug":   req.ProjectName,
		"labels": map[string]string{"custom.kind": "9router-relay"},
		"config": map[string]any{
			"install": "deno install",
			"runtime": map[string]any{"type": "dynamic", "entrypoint": "main.ts"},
		},
	})
	createReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, denoAPI+"/apps", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer "+req.DenoToken)
	createReq.Header.Set("Content-Type", "application/json")

	createRes, err := http.DefaultClient.Do(createReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	defer createRes.Body.Close()

	if createRes.StatusCode == http.StatusConflict {
		writeJSON(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf(`App "%s" already exists. Choose a different name.`, req.ProjectName)})
		return
	}

	if createRes.StatusCode < 200 || createRes.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(createRes.Body, 4096))
		writeJSON(w, createRes.StatusCode, map[string]any{"error": fmt.Sprintf("Failed to create app (%d): %s", createRes.StatusCode, text)})

		return
	}

	var app struct {
		ID string `json:"id"`
	}

	_ = json.NewDecoder(createRes.Body).Decode(&app)

	deployBody, _ := json.Marshal(map[string]any{
		"assets": map[string]any{
			"main.ts": map[string]any{
				"kind": "file", "content": denoRelayCode, "encoding": "utf-8",
			},
		},
	})
	deployReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, denoAPI+"/apps/"+app.ID+"/deploy", bytes.NewReader(deployBody))
	deployReq.Header.Set("Authorization", "Bearer "+req.DenoToken)
	deployReq.Header.Set("Content-Type", "application/json")

	deployRes, err := http.DefaultClient.Do(deployReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	defer deployRes.Body.Close()

	if deployRes.StatusCode < 200 || deployRes.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(deployRes.Body, 4096))
		// cleanup app
		del, _ := http.NewRequestWithContext(r.Context(), http.MethodDelete, denoAPI+"/apps/"+app.ID, nil)
		del.Header.Set("Authorization", "Bearer "+req.DenoToken)

		if res, e := http.DefaultClient.Do(del); e == nil {
			res.Body.Close()
		}

		writeJSON(w, deployRes.StatusCode, map[string]any{"error": fmt.Sprintf("Deploy failed (%d): %s", deployRes.StatusCode, text)})

		return
	}

	var revision struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

	_ = json.NewDecoder(deployRes.Body).Decode(&revision)

	status := revision.Status
	for i := 0; i < 30 && (status == "queued" || status == "building"); i++ {
		time.Sleep(2 * time.Second)

		stReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, denoAPI+"/revisions/"+revision.ID, nil)
		stReq.Header.Set("Authorization", "Bearer "+req.DenoToken)

		stRes, err := http.DefaultClient.Do(stReq)
		if err != nil {
			break
		}

		var stData struct {
			Status string `json:"status"`
		}

		_ = json.NewDecoder(stRes.Body).Decode(&stData)
		stRes.Body.Close()

		status = stData.Status
	}

	if status != "succeeded" {
		del, _ := http.NewRequestWithContext(r.Context(), http.MethodDelete, denoAPI+"/apps/"+app.ID, nil)
		del.Header.Set("Authorization", "Bearer "+req.DenoToken)

		if res, e := http.DefaultClient.Do(del); e == nil {
			res.Body.Close()
		}

		if status == "queued" || status == "building" {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Deploy timed out after 60 seconds"})
			return
		}

		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Deploy failed with status: " + status})

		return
	}

	orgSlug := strings.Split(req.OrgDomain, ".")[0]
	deployURL := fmt.Sprintf("https://%s.%s.deno.net", req.ProjectName, orgSlug)

	pool, err := s.storeRelayPool(req.ProjectName, "deno", deployURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}

// POST /api/proxy-pools/vercel-deploy.
func (s *Server) handleVercelDeploy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		VercelToken string `json:"vercelToken"`
		ProjectName string `json:"projectName"`
		DryRun      bool   `json:"dryRun"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	req.VercelToken = strings.TrimSpace(req.VercelToken)
	req.ProjectName = strings.TrimSpace(req.ProjectName)

	if req.ProjectName == "" {
		req.ProjectName = defaultRelayName()
	}

	if req.DryRun {
		writeJSONOK(w, map[string]any{
			"script": strings.TrimSpace(vercelRelayCode), "projectName": req.ProjectName,
			"instructions": "POST /v13/deployments with api/relay.js + vercel.json rewrite.",
		})

		return
	}

	if req.VercelToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Vercel API token is required"})
		return
	}

	const vercelAPI = "https://api.vercel.com"

	pkgJSON, _ := json.Marshal(map[string]any{"name": req.ProjectName, "version": "1.0.0"})
	vercelJSON, _ := json.Marshal(map[string]any{
		"rewrites": []map[string]string{{"source": "/(.*)", "destination": "/api/relay"}},
	})
	deployPayload, _ := json.Marshal(map[string]any{
		"name": req.ProjectName,
		"files": []map[string]string{
			{"file": "api/relay.js", "data": strings.TrimSpace(vercelRelayCode) + "\n"},
			{"file": "package.json", "data": string(pkgJSON)},
			{"file": "vercel.json", "data": string(vercelJSON)},
		},
		"projectSettings": map[string]any{"framework": nil},
		"target":          "production",
	})
	deployReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, vercelAPI+"/v13/deployments", bytes.NewReader(deployPayload))
	deployReq.Header.Set("Authorization", "Bearer "+req.VercelToken)
	deployReq.Header.Set("Content-Type", "application/json")

	deployRes, err := http.DefaultClient.Do(deployReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}

	defer deployRes.Body.Close()

	if deployRes.StatusCode < 200 || deployRes.StatusCode >= 300 {
		var errBody map[string]any
		_ = json.NewDecoder(deployRes.Body).Decode(&errBody)
		msg := "Failed to create Vercel deployment"

		if e, ok := errBody["error"].(map[string]any); ok {
			if m, ok := e["message"].(string); ok && m != "" {
				msg = m
			}
		}

		writeJSON(w, deployRes.StatusCode, map[string]any{"error": msg})

		return
	}

	var deployment struct {
		ID        string `json:"id"`
		UID       string `json:"uid"`
		ProjectID string `json:"projectId"`
		URL       string `json:"url"`
	}

	_ = json.NewDecoder(deployRes.Body).Decode(&deployment)

	deploymentID := deployment.ID
	if deploymentID == "" {
		deploymentID = deployment.UID
	}

	projectID := deployment.ProjectID
	if projectID == "" {
		projectID = req.ProjectName
	}
	// disable SSO protection (best-effort)
	patchBody, _ := json.Marshal(map[string]any{"ssoProtection": nil})
	patchReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPatch, vercelAPI+"/v9/projects/"+projectID, bytes.NewReader(patchBody))
	patchReq.Header.Set("Authorization", "Bearer "+req.VercelToken)
	patchReq.Header.Set("Content-Type", "application/json")

	if res, err := http.DefaultClient.Do(patchReq); err == nil {
		res.Body.Close()
	}

	// poll ready
	deadline := time.Now().Add(120 * time.Second)

	var readyURL string

	for time.Now().Before(deadline) {
		stReq, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, vercelAPI+"/v13/deployments/"+deploymentID, nil)
		stReq.Header.Set("Authorization", "Bearer "+req.VercelToken)

		stRes, err := http.DefaultClient.Do(stReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}

		var stData struct {
			ReadyState string `json:"readyState"`
			URL        string `json:"url"`
		}

		_ = json.NewDecoder(stRes.Body).Decode(&stData)
		stRes.Body.Close()

		if stData.ReadyState == "READY" {
			readyURL = stData.URL
			break
		}

		if stData.ReadyState == "ERROR" || stData.ReadyState == "CANCELED" {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Deployment failed: " + stData.ReadyState})
			return
		}

		time.Sleep(3 * time.Second)
	}

	if readyURL == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Deployment timed out"})
		return
	}

	deployURL := "https://" + readyURL

	pool, err := s.storeRelayPool(req.ProjectName, "vercel", deployURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}
