package oauth

import (
	"testing"
	"time"
)

func TestShouldRefresh_ExpiredSoon(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(2 * time.Minute)

	if !ShouldRefresh("claude", expires, time.Time{}, 0, lead) {
		t.Fatal("want refresh when expires within lead")
	}
}

func TestShouldRefresh_AlreadyExpired(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(-1 * time.Minute)

	if !ShouldRefresh("claude", expires, time.Time{}, 0, lead) {
		t.Fatal("want refresh when already expired")
	}
}

func TestShouldRefresh_Fresh(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(30 * time.Minute)

	if ShouldRefresh("claude", expires, time.Time{}, 0, lead) {
		t.Fatal("no refresh when far from expiry")
	}
}

func TestShouldRefresh_MaxAgeStale(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(30 * time.Minute) // not near expiry
	maxAge := 8 * 24 * time.Hour
	last := now.Add(-9 * 24 * time.Hour)

	if !ShouldRefresh("codex", expires, last, maxAge, lead) {
		t.Fatal("want refresh when lastRefresh beyond maxAge")
	}
}

func TestShouldRefresh_MaxAgeMissingLast(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(30 * time.Minute)
	maxAge := 8 * 24 * time.Hour

	if !ShouldRefresh("codex", expires, time.Time{}, maxAge, lead) {
		t.Fatal("want refresh when maxAge set but lastRefresh zero")
	}
}

func TestShouldRefresh_MaxAgeFresh(t *testing.T) {
	now := time.Now()
	lead := 5 * time.Minute
	expires := now.Add(30 * time.Minute)
	maxAge := 8 * 24 * time.Hour
	last := now.Add(-1 * time.Hour)

	if ShouldRefresh("codex", expires, last, maxAge, lead) {
		t.Fatal("no refresh when lastRefresh within maxAge and not near expiry")
	}
}

func TestShouldRefresh_ZeroExpiresNoMaxAge(t *testing.T) {
	if ShouldRefresh("claude", time.Time{}, time.Time{}, 0, 5*time.Minute) {
		t.Fatal("no refresh when no expiry and no maxAge")
	}
}

func TestMergeRefreshed(t *testing.T) {
	cur := map[string]any{
		"accessToken":  "old",
		"refreshToken": "rt-old",
		"idToken":      "id-old",
		"providerSpecificData": map[string]any{
			"projectId": "p1",
		},
	}
	next := map[string]any{
		"accessToken": "new",
		"expiresIn":   float64(3600),
		"providerSpecificData": map[string]any{
			"region": "us",
		},
	}

	out := MergeRefreshed(cur, next)
	if out["accessToken"] != "new" {
		t.Fatalf("accessToken=%v", out["accessToken"])
	}

	if out["refreshToken"] != "rt-old" {
		t.Fatalf("refreshToken kept=%v", out["refreshToken"])
	}

	if out["idToken"] != "id-old" {
		t.Fatalf("idToken kept=%v", out["idToken"])
	}

	if out["expiresAt"] == nil || out["expiresAt"] == "" {
		t.Fatal("expiresAt from expiresIn")
	}

	psd, _ := out["providerSpecificData"].(map[string]any)
	if psd["projectId"] != "p1" || psd["region"] != "us" {
		t.Fatalf("psd merge=%v", psd)
	}

	if out["lastRefreshAt"] == nil || out["lastRefreshAt"] == "" {
		t.Fatal("lastRefreshAt stamped")
	}
}
