package usage

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDecodeGrokCreditsFrame(t *testing.T) {
	t.Run("empty buffer", func(t *testing.T) {
		pct, reset, ok := DecodeGrokCreditsFrame(nil)
		if ok || pct != 0 || reset != nil {
			t.Fatalf("expected failure for nil buffer")
		}
	})

	t.Run("valid framed payload", func(t *testing.T) {
		ratioBytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(ratioBytes, 0x3eb33333)

		tsBytes := []byte{
			(1 << 3) | 0, 0xd4, 0x88, 0x88, 0xd3, 0x06,
			(2 << 3) | 0, 0x90, 0xb8, 0xb9, 0x9d, 0x03,
		}
		tsField := append([]byte{(5 << 3) | 2, byte(len(tsBytes))}, tsBytes...)

		nestedBody := append([]byte{(1 << 3) | 5}, ratioBytes...)
		nestedBody = append(nestedBody, tsField...)

		topBody := append([]byte{(1 << 3) | 2, byte(len(nestedBody))}, nestedBody...)

		header := make([]byte, 5)
		header[0] = 0x00
		binary.BigEndian.PutUint32(header[1:5], uint32(len(topBody)))

		fullFrame := append(header, topBody...)

		pct, reset, ok := DecodeGrokCreditsFrame(fullFrame)
		if !ok {
			t.Fatalf("expected success")
		}
		if pct < 34.9 || pct > 35.1 {
			t.Fatalf("expected ~35%%, got %f", pct)
		}
		if reset == nil {
			t.Fatalf("expected reset timestamp")
		}
	})
}

func TestFetchProviderUsageRouting(t *testing.T) {
	providers := []string{
		"github", "gemini-cli", "antigravity", "claude", "codex", "kiro",
		"qoder", "iflow", "ollama", "glm", "glm-cn",
		"minimax", "minimax-cn", "vercel-ai-gateway", "grok-cli", "kimi",
		"deepseek", "codebuddy-cn", "codebuddy-intl",
	}

	for _, p := range providers {
		t.Run(p, func(t *testing.T) {
			res := FetchProviderUsage(context.Background(), FetchOptions{
				Provider: p,
			})
			if res == nil {
				t.Fatalf("expected non-nil response for %s", p)
			}
			if res.Message == "Usage API not implemented for "+p {
				t.Fatalf("unregistered provider handler: %s", p)
			}
		})
	}

	t.Run("unsupported provider", func(t *testing.T) {
		res := FetchProviderUsage(context.Background(), FetchOptions{
			Provider: "unknown-provider-xyz",
		})
		if res.Message != "Usage API not implemented for unknown-provider-xyz" {
			t.Fatalf("unexpected message: %s", res.Message)
		}
	})
}

func TestGitHubUsageParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"copilot_plan": "individual",
			"quota_reset_date": "2026-08-01T00:00:00Z",
			"quota_snapshots": {
				"chat": {"entitlement": 100, "remaining": 80, "unlimited": false},
				"completions": {"entitlement": 200, "remaining": 50, "unlimited": false}
			}
		}`))
	}))
	defer ts.Close()

	opts := FetchOptions{
		Provider:    "github",
		AccessToken: "gh-test-token",
		BaseURL:     ts.URL,
		HTTPClient:  ts.Client(),
	}

	res := FetchProviderUsage(context.Background(), opts)
	if res.Plan != "individual" {
		t.Fatalf("expected plan individual, got %s", res.Plan)
	}
	if len(res.Quotas) != 2 {
		t.Fatalf("expected 2 quotas, got %d", len(res.Quotas))
	}
	chat := res.Quotas["chat"]
	if chat.Used != 20 || chat.Total != 100 || chat.Remaining != 80 {
		t.Fatalf("unexpected chat quota: %+v", chat)
	}
}

func TestClaudeUsageParsing(t *testing.T) {
	item5h := makeClaudeQuotaObject(42.0, "2026-08-01T05:00:00Z")
	if item5h.Used != 42.0 || item5h.Total != 100 || item5h.RemainingPercentage != 58.0 {
		t.Fatalf("unexpected claude 5h item: %+v", item5h)
	}
}

func TestCodexUsageParsing(t *testing.T) {
	data := map[string]any{
		"plan_type": "team",
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"used_percent": 30.0,
				"reset_at":     "2026-08-01T05:00:00Z",
			},
		},
	}
	quotas := make(map[string]QuotaItem)
	appendCodexQuotaWindows(quotas, "", extractCodexRateLimit(data))
	sess := quotas["session"]
	if sess.Used != 30 || sess.Remaining != 70 {
		t.Fatalf("unexpected codex session: %+v", sess)
	}
}

func TestKiroUsageParsing(t *testing.T) {
	data := map[string]any{
		"subscriptionInfo": map[string]any{
			"subscriptionTitle": "Kiro Pro",
		},
		"nextDateReset": "2026-08-01T00:00:00Z",
		"usageBreakdownList": []any{
			map[string]any{
				"resourceType":              "AGENTIC_REQUEST",
				"currentUsageWithPrecision": 12.0,
				"usageLimitWithPrecision":   100.0,
			},
		},
	}
	res := parseKiroQuotaData(data)
	if res.Plan != "Kiro Pro" {
		t.Fatalf("expected Kiro Pro, got %s", res.Plan)
	}
	agentic := res.Quotas["agentic_request"]
	if agentic.Used != 12 || agentic.Total != 100 || agentic.Remaining != 88 {
		t.Fatalf("unexpected kiro quota: %+v", agentic)
	}
}

func TestQoderUsageParsing(t *testing.T) {
	var uRes QuotaResult
	uRes.Quotas = map[string]QuotaItem{
		"user": {
			Used:      250.0,
			Total:     1000.0,
			Remaining: 750.0,
			Unit:      "credits",
		},
	}
	computeTopLevelNormalized(&uRes)
	if uRes.Limit != 1000 || uRes.Used != 250 || uRes.Remaining != 750 {
		t.Fatalf("unexpected normalized top level: %+v", uRes)
	}
}

func TestDeepseekUsageParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"is_available": true,
			"balance_infos": [
				{"currency": "USD", "total_balance": "12.50"}
			]
		}`))
	}))
	defer ts.Close()

	opts := FetchOptions{
		Provider:   "deepseek",
		APIKey:     "sk-test-key",
		BaseURL:    ts.URL,
		HTTPClient: ts.Client(),
	}

	res := FetchProviderUsage(context.Background(), opts)
	if res.Plan != "DeepSeek" {
		t.Fatalf("expected plan DeepSeek, got %s", res.Plan)
	}
	usd := res.Quotas["Balance (USD)"]
	if usd.Total != 12.5 || usd.RemainingPercentage != 100 {
		t.Fatalf("unexpected usd quota: %+v", usd)
	}
}

func TestGrokCliBillingParsing(t *testing.T) {
	billing := map[string]any{
		"config": map[string]any{
			"onDemandCap":      map[string]any{"val": 100.0},
			"onDemandUsed":     map[string]any{"val": 35.0},
			"prepaidBalance":   map[string]any{"val": 12.5},
			"billingPeriodEnd": "2026-07-15T00:00:00Z",
		},
	}
	user := map[string]any{
		"hasGrokCodeAccess": true,
	}

	res := parseGrokCliBilling(billing, user)
	if res.Plan != "Grok Code" {
		t.Fatalf("expected plan Grok Code, got %s", res.Plan)
	}
	onDemand := res.Quotas["On-demand"]
	if onDemand.Used != 35 || onDemand.Total != 100 || onDemand.RemainingPercentage != 65 {
		t.Fatalf("unexpected On-demand quota: %+v", onDemand)
	}
	prepaid := res.Quotas["Prepaid"]
	if prepaid.Total != 12.5 || prepaid.RemainingPercentage != 100 {
		t.Fatalf("unexpected Prepaid quota: %+v", prepaid)
	}
}

func TestKimiUsageParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"user": {
				"membership": {"level": "LEVEL_ADVANCED"}
			},
			"usage": {
				"limit": 100,
				"used": 25,
				"remaining": 75,
				"resetTime": "2026-08-01T00:00:00Z"
			}
		}`))
	}))
	defer ts.Close()

	opts := FetchOptions{
		Provider:   "kimi",
		APIKey:     "kimi-test-key",
		BaseURL:    ts.URL,
		HTTPClient: ts.Client(),
	}

	res := FetchProviderUsage(context.Background(), opts)
	if res.Plan != "Allegro" {
		t.Fatalf("expected plan Allegro, got %s", res.Plan)
	}
	wk := res.Quotas["Weekly"]
	if wk.Used != 25 || wk.Total != 100 || wk.RemainingPercentage != 75 {
		t.Fatalf("unexpected Weekly quota: %+v", wk)
	}
}
