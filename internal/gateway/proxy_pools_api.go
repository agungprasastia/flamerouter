package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (s *Server) handleListProxyPools(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) handleCreateProxyPool(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		ProxyURL string `json:"proxyUrl"`
		IsActive *bool  `json:"isActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.Type == "" {
		req.Type = "http"
	}
	// accept proxyUrl form (9router)
	if req.Host == "" && strings.TrimSpace(req.ProxyURL) != "" {
		u, err := url.Parse(strings.TrimSpace(req.ProxyURL))
		if err != nil || u.Host == "" {
			writeErr(w, http.StatusBadRequest, "Invalid proxy URL")
			return
		}
		req.Host = u.Hostname()
		if u.Port() != "" {
			req.Port, _ = strconv.Atoi(u.Port())
		} else if u.Scheme == "https" {
			req.Port = 443
		} else {
			req.Port = 80
		}
		if u.User != nil {
			req.Username = u.User.Username()
			req.Password, _ = u.User.Password()
		}
		if req.Type == "http" && u.Scheme != "" {
			req.Type = u.Scheme
		}
	}
	if req.Host == "" {
		writeErr(w, http.StatusBadRequest, "Proxy URL is required")
		return
	}
	if req.Port == 0 {
		req.Port = 8080
	}
	id, err := s.st.CreateProxyPool(req.Name, req.Type, req.Host, req.Port, req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	if req.IsActive != nil && !*req.IsActive {
		_ = s.st.UpdateProxyPool(id, req.Name, req.Type, req.Host, req.Port, req.Username, req.Password, false)
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleUpdateProxyPool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		IsActive *bool  `json:"isActive"`
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

// POST /api/proxy-pools/{id}/test
func (s *Server) handleTestProxyPool(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pool, err := s.st.GetProxyPool(id)
	if err != nil || pool == nil {
		writeErr(w, http.StatusNotFound, "Proxy pool not found")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	started := time.Now()
	var result struct {
		ok         bool
		status     int
		statusText string
		err        string
	}

	typ := strings.ToLower(pool.Type)
	proxyURL := buildProxyURL(pool.Type, pool.Host, pool.Port, pool.Username, pool.Password)

	switch typ {
	case "vercel", "cloudflare", "deno":
		// relay: GET relay with x-relay-target headers
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
		if err != nil {
			result.err = err.Error()
		} else {
			req.Header.Set("x-relay-target", "https://httpbin.org")
			req.Header.Set("x-relay-path", "/get")
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				result.err = err.Error()
				result.status = 500
			} else {
				_ = res.Body.Close()
				result.ok = res.StatusCode >= 200 && res.StatusCode < 300
				result.status = res.StatusCode
				result.statusText = res.Status
				if !result.ok {
					result.err = fmt.Sprintf("Proxy test failed with status %d", res.StatusCode)
				}
			}
		}
	default:
		// HTTP/SOCKS-style: use as HTTP proxy to hit httpbin
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		u, err := url.Parse(proxyURL)
		if err != nil || u.Host == "" {
			result.err = "Invalid proxy URL"
			result.status = 500
		} else {
			transport := &http.Transport{
				Proxy: http.ProxyURL(u),
				DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
			}
			client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://httpbin.org/get", nil)
			if err != nil {
				result.err = err.Error()
			} else {
				res, err := client.Do(req)
				if err != nil {
					result.err = err.Error()
					result.status = 500
				} else {
					_ = res.Body.Close()
					result.ok = res.StatusCode >= 200 && res.StatusCode < 300
					result.status = res.StatusCode
					result.statusText = res.Status
					if !result.ok {
						result.err = fmt.Sprintf("Proxy test failed with status %d", res.StatusCode)
					}
				}
			}
		}
	}

	_ = s.st.UpdateProxyPoolTestStatus(id, result.ok)
	elapsed := time.Since(started).Milliseconds()
	writeJSONOK(w, map[string]any{
		"ok": result.ok, "status": result.status, "statusText": result.statusText,
		"error": result.err, "elapsedMs": elapsed, "testedAt": now,
	})
}

func buildProxyURL(typ, host string, port int, user, pass string) string {
	if strings.Contains(host, "://") {
		return host
	}
	scheme := typ
	if scheme == "" || scheme == "http" || scheme == "https" {
		if scheme == "" {
			scheme = "http"
		}
	} else if scheme == "socks5" || scheme == "socks" {
		scheme = "socks5"
	} else if scheme == "vercel" || scheme == "cloudflare" || scheme == "deno" {
		scheme = "https"
	} else {
		scheme = "http"
	}
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
