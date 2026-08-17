package gateway

import (
	"context"
	"encoding/json"
	"flamerouter/internal/netutil"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleListProxyPools(w http.ResponseWriter, _ *http.Request) {
	pools, err := s.st.ListProxyPools()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	list := make([]map[string]any, 0, len(pools))
	for _, p := range pools {
		list = append(list, map[string]any{
			"id": p.ID, "name": p.Name, "type": p.Type,
			"host": p.Host, "port": p.Port,
			"username": p.Username, "isActive": p.IsActive,
			// never return password hash-equivalent raw? 9router returns it; keep for edit form
			"password": p.Password,
		})
	}

	writeJSONOK(w, map[string]any{"proxyPools": list})
}

type createProxyPoolReq struct {
	IsActive *bool  `json:"isActive"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	ProxyURL string `json:"proxyUrl"`
	Port     int    `json:"port"`
}

func parseProxyURLField(rawURL string) (host string, port int, user, pass, scheme string, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return "", 0, "", "", "", fmt.Errorf("invalid proxy URL")
	}

	host = u.Hostname()

	switch {
	case u.Port() != "":
		port, _ = strconv.Atoi(u.Port()) //nolint:errcheck // port parsed by net/url
	case u.Scheme == "https":
		port = 443
	default:
		port = 80
	}

	if u.User != nil {
		user = u.User.Username()
		pass, _ = u.User.Password()
	}

	return host, port, user, pass, u.Scheme, nil
}

func normalizeCreateProxyPoolReq(req *createProxyPoolReq) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}

	if req.Type == "" {
		req.Type = "http"
	}

	if req.Host == "" && strings.TrimSpace(req.ProxyURL) != "" {
		h, p, u, pass, scheme, err := parseProxyURLField(req.ProxyURL)
		if err != nil {
			return err
		}

		req.Host = h
		req.Port = p
		req.Username = u
		req.Password = pass

		if req.Type == "http" && scheme != "" {
			req.Type = scheme
		}
	}

	if req.Host == "" {
		return fmt.Errorf("proxy URL is required")
	}

	if req.Port == 0 {
		req.Port = 8080
	}

	return nil
}

func (s *Server) handleCreateProxyPool(w http.ResponseWriter, r *http.Request) {
	var req createProxyPoolReq

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := normalizeCreateProxyPoolReq(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := s.st.CreateProxyPool(req.Name, req.Type, req.Host, req.Port, req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if req.IsActive != nil && !*req.IsActive {
		//nolint:errcheck // best-effort update
		_ = s.st.UpdateProxyPool(id, req.Name, req.Type, req.Host, req.Port, req.Username, req.Password, false)
	}

	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateProxyPool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		IsActive *bool  `json:"isActive"`
		Name     string `json:"name"`
		Type     string `json:"type"`
		Host     string `json:"host"`
		Username string `json:"username"`
		Password string `json:"password"`
		Port     int    `json:"port"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	active := true
	if req.IsActive != nil {
		active = *req.IsActive
	}

	if req.Type == "" {
		req.Type = "http"
	}

	if err := s.st.UpdateProxyPool(id, req.Name, req.Type, req.Host, req.Port, req.Username, req.Password, active); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "id": id})
}

func (s *Server) handleDeleteProxyPool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.st.DeleteProxyPool(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true})
}

type proxyTestResult struct {
	statusText string
	err        string
	status     int
	ok         bool
}

func testRelayProxy(ctx context.Context, proxyURL string) proxyTestResult {
	var res proxyTestResult

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		res.err = err.Error()
		return res
	}

	req.Header.Set("x-relay-target", "https://httpbin.org")
	req.Header.Set("x-relay-path", "/get")

	resp, err := netutil.DoHTTP(http.DefaultClient, req)
	if err != nil {
		res.err = err.Error()
		res.status = 500

		return res
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on ping response

	res.ok = resp.StatusCode >= 200 && resp.StatusCode < 300
	res.status = resp.StatusCode
	res.statusText = resp.Status

	if !res.ok {
		res.err = fmt.Sprintf("Proxy test failed with status %d", resp.StatusCode)
	}

	return res
}

func makeStandardProxyTransport(u *url.URL) *http.Transport {
	dialer := &net.Dialer{
		Timeout:         8 * time.Second,
		Deadline:        time.Time{},
		LocalAddr:       nil,
		DualStack:       false,
		FallbackDelay:   0,
		KeepAlive:       0,
		KeepAliveConfig: net.KeepAliveConfig{Enable: false, Idle: 0, Interval: 0, Count: 0},
		Resolver:        nil,
		Cancel:          nil,
		Control:         nil,
		ControlContext:  nil,
	}

	return &http.Transport{
		Proxy:                  http.ProxyURL(u),
		DialContext:            dialer.DialContext,
		OnProxyConnectResponse: nil,
		Dial:                   nil,
		DialTLSContext:         nil,
		DialTLS:                nil,
		TLSClientConfig:        nil,
		TLSHandshakeTimeout:    0,
		DisableKeepAlives:      false,
		DisableCompression:     false,
		MaxIdleConns:           0,
		MaxIdleConnsPerHost:    0,
		MaxConnsPerHost:        0,
		IdleConnTimeout:        0,
		ResponseHeaderTimeout:  0,
		ExpectContinueTimeout:  0,
		TLSNextProto:           nil,
		ProxyConnectHeader:     nil,
		GetProxyConnectHeader:  nil,
		MaxResponseHeaderBytes: 0,
		WriteBufferSize:        0,
		ReadBufferSize:         0,
		ForceAttemptHTTP2:      false,
		HTTP2:                  nil,
		Protocols:              nil,
	}
}

func testStandardProxy(ctx context.Context, proxyURL string) proxyTestResult {
	var res proxyTestResult

	u, err := url.Parse(proxyURL)
	if err != nil || u.Host == "" {
		res.err = "invalid proxy URL"
		res.status = 500

		return res
	}

	client := &http.Client{
		Transport:     makeStandardProxyTransport(u),
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://httpbin.org/get", nil)
	if err != nil {
		res.err = err.Error()
		return res
	}

	resp, err := netutil.DoHTTP(client, req)
	if err != nil {
		res.err = err.Error()
		res.status = 500

		return res
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close on test response

	res.ok = resp.StatusCode >= 200 && resp.StatusCode < 300
	res.status = resp.StatusCode
	res.statusText = resp.Status

	if !res.ok {
		res.err = fmt.Sprintf("Proxy test failed with status %d", resp.StatusCode)
	}

	return res
}

// POST /api/proxy-pools/{id}/test.
func (s *Server) handleTestProxyPool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	pool, err := s.st.GetProxyPool(id)
	if err != nil || pool == nil {
		writeErr(w, http.StatusNotFound, "Proxy pool not found")
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	started := time.Now()

	typ := strings.ToLower(pool.Type)
	proxyURL := buildProxyURL(pool.Type, pool.Host, pool.Port, pool.Username, pool.Password)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var result proxyTestResult

	switch typ {
	case "vercel", "cloudflare", "deno":
		result = testRelayProxy(ctx, proxyURL)
	default:
		result = testStandardProxy(ctx, proxyURL)
	}

	//nolint:errcheck // best-effort status update
	_ = s.st.UpdateProxyPoolTestStatus(id, result.ok)

	elapsed := time.Since(started).Milliseconds()
	writeJSONOK(w, map[string]any{
		"ok": result.ok, "status": result.status, "statusText": result.statusText,
		"error": result.err, "elapsedMs": elapsed, "testedAt": now,
	})
}

func resolveProxyScheme(typ string) string {
	scheme := strings.ToLower(typ)
	switch scheme {
	case "", "http", "https":
		if scheme == "" {
			return "http"
		}

		return scheme
	case "socks5", "socks":
		return "socks5"
	case "vercel", "cloudflare", "deno":
		return "https"
	default:
		return "http"
	}
}

func buildProxyURL(typ, host string, port int, user, pass string) string {
	if strings.Contains(host, "://") {
		return host
	}

	scheme := resolveProxyScheme(typ)
	auth := ""

	if user != "" {
		if pass != "" {
			auth = url.UserPassword(user, pass).String() + "@"
		} else {
			auth = url.User(user).String() + "@"
		}
	}

	if port <= 0 {
		return fmt.Sprintf("%s://%s%s", scheme, auth, host)
	}

	return fmt.Sprintf("%s://%s%s:%d", scheme, auth, host, port)
}
