package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/netutil"
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
		if p, errP := strconv.Atoi(u.Port()); errP == nil {
			port = p
		}
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

type cloudflareDeployReq struct {
	AccountID   string `json:"accountId"`
	APIToken    string `json:"apiToken"`
	ProjectName string `json:"projectName"`
	DryRun      bool   `json:"dryRun"`
}

func buildCloudflareWorkerBody() (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, errPart := mw.CreateFormFile("index.js", "index.js")
	if errPart != nil {
		return nil, "", errPart
	}

	if _, err := io.WriteString(part, strings.TrimSpace(cfRelayWorker)+"\n"); err != nil {
		return nil, "", err
	}

	metaPart, errMeta := mw.CreateFormFile("metadata", "metadata.json")
	if errMeta != nil {
		return nil, "", errMeta
	}

	meta, errMetaJSON := json.Marshal(map[string]any{
		"main_module":        "index.js",
		"compatibility_date": "2024-03-20",
		"observability":      map[string]any{"enabled": true},
	})
	if errMetaJSON != nil {
		return nil, "", errMetaJSON
	}

	if _, err := metaPart.Write(meta); err != nil {
		return nil, "", err
	}

	if err := mw.Close(); err != nil {
		return nil, "", err
	}

	return &buf, mw.FormDataContentType(), nil
}

func parseCloudflareUploadError(resp *http.Response) string {
	var errBody struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&errBody); err == nil && len(errBody.Errors) > 0 {
		if msg := errBody.Errors[0].Message; msg != "" {
			return msg
		}
	}

	return "Failed to upload Worker to Cloudflare"
}

func enableCloudflareSubdomain(ctx context.Context, workerURL, apiToken string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, workerURL+"/subdomain", strings.NewReader(`{"enabled":true}`))
	if err != nil {
		return
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	res, errDo := netutil.DoHTTP(http.DefaultClient, req)
	if errDo != nil || res == nil || res.Body == nil {
		return
	}

	defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort
}

func getCloudflareSubdomain(ctx context.Context, accountID, apiToken string) string {
	acctURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/subdomain", accountID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, acctURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)

	res, errDo := netutil.DoHTTP(http.DefaultClient, req)
	if errDo != nil || res == nil || res.Body == nil {
		return ""
	}

	defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort

	var data struct {
		Result struct {
			Subdomain string `json:"subdomain"`
		} `json:"result"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err == nil {
		return data.Result.Subdomain
	}

	return ""
}

func validateCloudflareDeployReq(req *cloudflareDeployReq) (bool, string, int) {
	if req.DryRun || req.APIToken == "" {
		if !req.DryRun && (req.AccountID == "" || req.APIToken == "") {
			return false, "Cloudflare Account ID and API Token are required", http.StatusBadRequest
		}

		return true, "", http.StatusOK
	}

	if req.AccountID == "" {
		return false, "Cloudflare Account ID and API Token are required", http.StatusBadRequest
	}

	return false, "", 0
}

func deployCloudflareWorker(ctx context.Context, req cloudflareDeployReq) (string, int, error) {
	workerURL := fmt.Sprintf("https://api.cloudflare.com/client/v4/accounts/%s/workers/scripts/%s", req.AccountID, req.ProjectName)

	buf, cType, errBuild := buildCloudflareWorkerBody()
	if errBuild != nil {
		return "", http.StatusInternalServerError, errBuild
	}

	uploadReq, errReq := http.NewRequestWithContext(ctx, http.MethodPut, workerURL, buf)
	if errReq != nil {
		return "", http.StatusInternalServerError, errReq
	}

	uploadReq.Header.Set("Authorization", "Bearer "+req.APIToken)
	uploadReq.Header.Set("Content-Type", cType)

	uploadRes, errDo := netutil.DoHTTP(http.DefaultClient, uploadReq)
	if errDo != nil {
		return "", http.StatusBadGateway, errDo
	}

	defer func() { _ = uploadRes.Body.Close() }() //nolint:errcheck // best effort

	if uploadRes.StatusCode < 200 || uploadRes.StatusCode >= 300 {
		msg := parseCloudflareUploadError(uploadRes)

		return "", uploadRes.StatusCode, fmt.Errorf("%s", msg)
	}

	enableCloudflareSubdomain(ctx, workerURL, req.APIToken)

	subdomain := getCloudflareSubdomain(ctx, req.AccountID, req.APIToken)
	if subdomain == "" {
		return "", http.StatusBadRequest, fmt.Errorf("worker deployed but failed to retrieve workers.dev subdomain: make sure you have setup a workers.dev subdomain in Cloudflare Dashboard")
	}

	return fmt.Sprintf("https://%s.%s.workers.dev", req.ProjectName, subdomain), http.StatusOK, nil
}

// POST /api/proxy-pools/cloudflare-deploy.
func (s *Server) handleCloudflareDeploy(w http.ResponseWriter, r *http.Request) {
	var req cloudflareDeployReq
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

	isDry, errMsg, status := validateCloudflareDeployReq(&req)
	if errMsg != "" {
		writeJSON(w, status, map[string]any{"error": errMsg})
		return
	}

	if isDry {
		writeJSON(w, http.StatusOK, map[string]any{
			"script":       strings.TrimSpace(cfRelayWorker),
			"projectName":  req.ProjectName,
			"instructions": "PUT workers/scripts via Cloudflare API with multipart metadata+index.js, then enable subdomain.",
		})

		return
	}

	deployURL, statusCode, errDeploy := deployCloudflareWorker(r.Context(), req)
	if errDeploy != nil {
		writeJSON(w, statusCode, map[string]any{"error": errDeploy.Error()})
		return
	}

	pool, errPool := s.storeRelayPool(req.ProjectName, "cloudflare", deployURL)
	if errPool != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": errPool.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}

type denoDeployReq struct {
	DenoToken   string `json:"denoToken"`
	OrgDomain   string `json:"orgDomain"`
	ProjectName string `json:"projectName"`
	DryRun      bool   `json:"dryRun"`
}

func parseDenoAppResponse(res *http.Response, projectName string) (string, int, error) {
	if res.StatusCode == http.StatusConflict {
		return "", http.StatusConflict, fmt.Errorf("app %q already exists, choose a different name", projectName)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		text, errRead := io.ReadAll(io.LimitReader(res.Body, 4096))
		if errRead != nil {
			return "", res.StatusCode, fmt.Errorf("failed to create app (%d)", res.StatusCode)
		}

		return "", res.StatusCode, fmt.Errorf("failed to create app (%d): %s", res.StatusCode, text)
	}

	var app struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(res.Body).Decode(&app); err != nil {
		return "", http.StatusInternalServerError, err
	}

	return app.ID, http.StatusOK, nil
}

func createDenoApp(ctx context.Context, token, projectName string) (string, int, error) {
	createBody, errMarshal := json.Marshal(map[string]any{
		"slug":   projectName,
		"labels": map[string]string{"custom.kind": "9router-relay"},
		"config": map[string]any{
			"install": "deno install",
			"runtime": map[string]any{"type": "dynamic", "entrypoint": "main.ts"},
		},
	})
	if errMarshal != nil {
		return "", http.StatusInternalServerError, errMarshal
	}

	req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deno.com/v2/apps", bytes.NewReader(createBody))
	if errReq != nil {
		return "", http.StatusInternalServerError, errReq
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, errDo := netutil.DoHTTP(http.DefaultClient, req)
	if errDo != nil {
		return "", http.StatusBadGateway, errDo
	}

	defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort

	return parseDenoAppResponse(res, projectName)
}

func deleteDenoApp(ctx context.Context, token, appID string) {
	del, err := http.NewRequestWithContext(ctx, http.MethodDelete, "https://api.deno.com/v2/apps/"+appID, nil)
	if err != nil {
		return
	}

	del.Header.Set("Authorization", "Bearer "+token)

	res, e := netutil.DoHTTP(http.DefaultClient, del)
	if e == nil && res != nil && res.Body != nil {
		defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort
	}
}

func deployDenoAppRevision(ctx context.Context, token, appID string) (string, error) {
	deployBody, errMarshal := json.Marshal(map[string]any{
		"assets": map[string]any{
			"main.ts": map[string]any{
				"kind": "file", "content": denoRelayCode, "encoding": "utf-8",
			},
		},
	})
	if errMarshal != nil {
		return "", errMarshal
	}

	deployReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deno.com/v2/apps/"+appID+"/deploy", bytes.NewReader(deployBody))
	if errReq != nil {
		return "", errReq
	}

	deployReq.Header.Set("Authorization", "Bearer "+token)
	deployReq.Header.Set("Content-Type", "application/json")

	deployRes, errDo := netutil.DoHTTP(http.DefaultClient, deployReq)
	if errDo != nil {
		return "", errDo
	}

	defer func() { _ = deployRes.Body.Close() }() //nolint:errcheck // best effort

	if deployRes.StatusCode < 200 || deployRes.StatusCode >= 300 {
		text, errRead := io.ReadAll(io.LimitReader(deployRes.Body, 4096))
		if errRead != nil {
			return "", fmt.Errorf("deploy failed (%d)", deployRes.StatusCode)
		}

		return "", fmt.Errorf("deploy failed (%d): %s", deployRes.StatusCode, text)
	}

	var revision struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}

	if err := json.NewDecoder(deployRes.Body).Decode(&revision); err != nil {
		return "", err
	}

	return revision.ID, nil
}

func pollDenoRevision(ctx context.Context, token, revisionID string) string {
	status := "queued"

	for i := 0; i < 30 && (status == "queued" || status == "building"); i++ {
		time.Sleep(2 * time.Second)

		stReq, errSt := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deno.com/v2/revisions/"+revisionID, nil)
		if errSt != nil {
			break
		}

		stReq.Header.Set("Authorization", "Bearer "+token)

		stRes, errDo := netutil.DoHTTP(http.DefaultClient, stReq)
		if errDo != nil || stRes == nil || stRes.Body == nil {
			break
		}

		var stData struct {
			Status string `json:"status"`
		}

		errDecode := json.NewDecoder(stRes.Body).Decode(&stData)
		_ = stRes.Body.Close() //nolint:errcheck // best effort

		if errDecode != nil {
			break
		}

		status = stData.Status
	}

	return status
}

func validateDenoDeployReq(req *denoDeployReq) (bool, string) {
	if req.DryRun {
		return true, ""
	}

	if req.OrgDomain == "" {
		return false, "Organization domain is required"
	}

	if req.DenoToken == "" {
		return false, "Deno Deploy API token is required"
	}

	return false, ""
}

func executeDenoDeployment(ctx context.Context, req denoDeployReq) (string, int, error) {
	appID, statusCo, errCreate := createDenoApp(ctx, req.DenoToken, req.ProjectName)
	if errCreate != nil {
		return "", statusCo, errCreate
	}

	revisionID, errDeploy := deployDenoAppRevision(ctx, req.DenoToken, appID)
	if errDeploy != nil {
		deleteDenoApp(ctx, req.DenoToken, appID)
		return "", http.StatusBadRequest, errDeploy
	}

	status := pollDenoRevision(ctx, req.DenoToken, revisionID)
	if status != "succeeded" {
		deleteDenoApp(ctx, req.DenoToken, appID)

		if status == "queued" || status == "building" {
			return "", http.StatusInternalServerError, fmt.Errorf("deploy timed out after 60 seconds")
		}

		return "", http.StatusInternalServerError, fmt.Errorf("deploy failed with status: %s", status)
	}

	orgSlug := strings.Split(req.OrgDomain, ".")[0]

	return fmt.Sprintf("https://%s.%s.deno.net", req.ProjectName, orgSlug), http.StatusOK, nil
}

// POST /api/proxy-pools/deno-deploy.
func (s *Server) handleDenoDeploy(w http.ResponseWriter, r *http.Request) {
	var req denoDeployReq
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

	isDry, errMsg := validateDenoDeployReq(&req)
	if errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errMsg})
		return
	}

	if isDry {
		writeJSONOK(w, map[string]any{
			"script": strings.TrimSpace(denoRelayCode), "projectName": req.ProjectName,
			"instructions": "Create Deno Deploy app then POST /apps/{id}/deploy with main.ts asset.",
		})

		return
	}

	deployURL, statusCo, errDeploy := executeDenoDeployment(r.Context(), req)
	if errDeploy != nil {
		writeJSON(w, statusCo, map[string]any{"error": errDeploy.Error()})
		return
	}

	pool, errPool := s.storeRelayPool(req.ProjectName, "deno", deployURL)
	if errPool != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": errPool.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}

type vercelDeployReq struct {
	VercelToken string `json:"vercelToken"`
	ProjectName string `json:"projectName"`
	DryRun      bool   `json:"dryRun"`
}

func buildVercelDeployPayload(projectName string) ([]byte, error) {
	pkgJSON, errPkg := json.Marshal(map[string]any{"name": projectName, "version": "1.0.0"})
	if errPkg != nil {
		return nil, errPkg
	}

	vercelJSON, errVercel := json.Marshal(map[string]any{
		"rewrites": []map[string]string{{"source": "/(.*)", "destination": "/api/relay"}},
	})
	if errVercel != nil {
		return nil, errVercel
	}

	return json.Marshal(map[string]any{
		"name": projectName,
		"files": []map[string]string{
			{"file": "api/relay.js", "data": strings.TrimSpace(vercelRelayCode) + "\n"},
			{"file": "package.json", "data": string(pkgJSON)},
			{"file": "vercel.json", "data": string(vercelJSON)},
		},
		"projectSettings": map[string]any{"framework": nil},
		"target":          "production",
	})
}

func parseVercelDeployError(res *http.Response) error {
	var errBody map[string]any
	if err := json.NewDecoder(res.Body).Decode(&errBody); err != nil {
		return fmt.Errorf("failed to create Vercel deployment: %w", err)
	}

	msg := "failed to create Vercel deployment"

	if e, ok := errBody["error"].(map[string]any); ok {
		if m, okStr := e["message"].(string); okStr && m != "" {
			msg = m
		}
	}

	return fmt.Errorf("%s", msg)
}

func parseVercelDeploymentResult(res *http.Response, projectName string) (string, string, error) {
	var deployment struct {
		ID        string `json:"id"`
		UID       string `json:"uid"`
		ProjectID string `json:"projectId"`
	}

	if err := json.NewDecoder(res.Body).Decode(&deployment); err != nil {
		return "", "", err
	}

	deploymentID := deployment.ID
	if deploymentID == "" {
		deploymentID = deployment.UID
	}

	projectID := deployment.ProjectID
	if projectID == "" {
		projectID = projectName
	}

	return deploymentID, projectID, nil
}

func createVercelDeployment(ctx context.Context, token, projectName string) (string, string, error) {
	payload, errPayload := buildVercelDeployPayload(projectName)
	if errPayload != nil {
		return "", "", errPayload
	}

	deployReq, errReq := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.vercel.com/v13/deployments", bytes.NewReader(payload))
	if errReq != nil {
		return "", "", errReq
	}

	deployReq.Header.Set("Authorization", "Bearer "+token)
	deployReq.Header.Set("Content-Type", "application/json")

	deployRes, errDo := netutil.DoHTTP(http.DefaultClient, deployReq)
	if errDo != nil {
		return "", "", errDo
	}

	defer func() { _ = deployRes.Body.Close() }() //nolint:errcheck // best effort

	if deployRes.StatusCode < 200 || deployRes.StatusCode >= 300 {
		return "", "", parseVercelDeployError(deployRes)
	}

	return parseVercelDeploymentResult(deployRes, projectName)
}

func disableVercelSSO(ctx context.Context, token, projectID string) {
	patchBody, err := json.Marshal(map[string]any{"ssoProtection": nil})
	if err != nil {
		return
	}

	patchReq, errReq := http.NewRequestWithContext(ctx, http.MethodPatch, "https://api.vercel.com/v9/projects/"+projectID, bytes.NewReader(patchBody))
	if errReq == nil {
		patchReq.Header.Set("Authorization", "Bearer "+token)
		patchReq.Header.Set("Content-Type", "application/json")

		res, e := netutil.DoHTTP(http.DefaultClient, patchReq)
		if e == nil && res != nil && res.Body != nil {
			defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort
		}
	}
}

func pollVercelReady(ctx context.Context, token, deploymentID string) (string, error) {
	deadline := time.Now().Add(120 * time.Second)

	for time.Now().Before(deadline) {
		stReq, errSt := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.vercel.com/v13/deployments/"+deploymentID, nil)
		if errSt != nil {
			return "", errSt
		}

		stReq.Header.Set("Authorization", "Bearer "+token)

		stRes, errDo := netutil.DoHTTP(http.DefaultClient, stReq)
		if errDo != nil {
			return "", errDo
		}

		var stData struct {
			ReadyState string `json:"readyState"`
			URL        string `json:"url"`
		}

		errDecode := json.NewDecoder(stRes.Body).Decode(&stData)
		_ = stRes.Body.Close() //nolint:errcheck // best effort

		if errDecode != nil {
			return "", errDecode
		}

		if stData.ReadyState == "READY" {
			return stData.URL, nil
		}

		if stData.ReadyState == "ERROR" || stData.ReadyState == "CANCELED" {
			return "", fmt.Errorf("deployment failed: %s", stData.ReadyState)
		}

		time.Sleep(3 * time.Second)
	}

	return "", fmt.Errorf("deployment timed out")
}

func validateVercelDeployReq(req *vercelDeployReq) (bool, string) {
	if req.DryRun {
		return true, ""
	}

	if req.VercelToken == "" {
		return false, "Vercel API token is required"
	}

	return false, ""
}

func executeVercelDeployment(ctx context.Context, req vercelDeployReq) (string, error) {
	deploymentID, projectID, errDeploy := createVercelDeployment(ctx, req.VercelToken, req.ProjectName)
	if errDeploy != nil {
		return "", errDeploy
	}

	disableVercelSSO(ctx, req.VercelToken, projectID)

	readyURL, errPoll := pollVercelReady(ctx, req.VercelToken, deploymentID)
	if errPoll != nil {
		return "", errPoll
	}

	return "https://" + readyURL, nil
}

// POST /api/proxy-pools/vercel-deploy.
func (s *Server) handleVercelDeploy(w http.ResponseWriter, r *http.Request) {
	var req vercelDeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json"})
		return
	}

	req.VercelToken = strings.TrimSpace(req.VercelToken)
	req.ProjectName = strings.TrimSpace(req.ProjectName)

	if req.ProjectName == "" {
		req.ProjectName = defaultRelayName()
	}

	isDry, errMsg := validateVercelDeployReq(&req)
	if errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errMsg})
		return
	}

	if isDry {
		writeJSONOK(w, map[string]any{
			"script": strings.TrimSpace(vercelRelayCode), "projectName": req.ProjectName,
			"instructions": "POST /v13/deployments with api/relay.js + vercel.json rewrite.",
		})

		return
	}

	deployURL, errDeploy := executeVercelDeployment(r.Context(), req)
	if errDeploy != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": errDeploy.Error()})
		return
	}

	pool, errPool := s.storeRelayPool(req.ProjectName, "vercel", deployURL)
	if errPool != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": errPool.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"proxyPool": pool, "deployUrl": deployURL})
}
