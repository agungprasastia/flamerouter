package auth

import (
	"bytes"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestProgressiveLock(t *testing.T) {
	ResetLoginLimiterForTest()

	ip := "1.2.3.4"
	for i := 0; i < 5; i++ {
		RecordFail(ip)
	}

	locked, retry := CheckLock(ip)
	if !locked || retry < 25 || retry > 35 {
		t.Fatalf("want ~30s lock, got locked=%v retry=%d", locked, retry)
	}

	RecordSuccess(ip)

	locked, _ = CheckLock(ip)
	if locked {
		t.Fatal("success clears lock")
	}
}

func TestProgressiveLock_Escalates(t *testing.T) {
	ResetLoginLimiterForTest()

	ip := "5.6.7.8"
	// first lock: 30s
	for i := 0; i < 5; i++ {
		RecordFail(ip)
	}

	locked, retry := CheckLock(ip)
	if !locked || retry < 25 || retry > 35 {
		t.Fatalf("level0 want ~30s, locked=%v retry=%d", locked, retry)
	}
	// expire lock by success-clear then re-fail to next level via internal state:
	// after lock, fails reset; next 5 fails → 2m
	// simulate unlock by recording success then re-building level via 5 fails twice
	// Actually: lockLevel increments on each lock. After first lock, fails=0.
	// We need lock to expire without clearing level. Use force-unlock helper via time?
	// Parity: once locked, CheckLock returns locked until lockUntil.
	// Manually: RecordSuccess clears all. To test escalate we need lock to end without success.
	// Expose test hook: clearLockUntil only
	clearLockUntilForTest(ip)

	for i := 0; i < 5; i++ {
		RecordFail(ip)
	}

	locked, retry = CheckLock(ip)
	if !locked || retry < 100 || retry > 140 {
		t.Fatalf("level1 want ~120s, locked=%v retry=%d", locked, retry)
	}
}

func TestRecordFail_RemainingBeforeLock(t *testing.T) {
	ResetLoginLimiterForTest()

	ip := "9.9.9.9"

	r := RecordFail(ip)
	if r != 4 {
		t.Fatalf("after 1 fail remaining want 4 got %d", r)
	}

	for i := 0; i < 3; i++ {
		RecordFail(ip)
	}

	r = RecordFail(ip) // 5th → lock, fails reset to 0 → remaining 5
	if r != 5 {
		t.Fatalf("after lock remaining want 5 got %d", r)
	}
}

func TestLogin_FifthFailReturns429(t *testing.T) {
	ResetLoginLimiterForTest()

	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if clErr := st.Close(); clErr != nil {
			t.Errorf("store close error: %v", clErr)
		}
	})

	sh := NewSessionHandler(NewJWTManager("test-secret"), st, "correct-pass")

	body := []byte(`{"password":"wrong"}`)
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		req.Header.Set("x-9r-real-ip", "203.0.113.50")
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		sh.Login(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("fail %d: want 401 got %d", i+1, rr.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("x-9r-real-ip", "203.0.113.50")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	sh.Login(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("5th fail: want 429 got %d body=%s", rr.Code, rr.Body.String())
	}

	ra := rr.Header().Get("Retry-After")
	sec, err := strconv.Atoi(ra)

	if err != nil || sec < 25 || sec > 35 {
		t.Fatalf("Retry-After want ~30 got %q", ra)
	}
}

func TestClientIP_PreferX9R(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("x-9r-real-ip", "10.0.0.1")
	r.Header.Set("X-Forwarded-For", "8.8.8.8")

	if got := ClientIP(r); got != "10.0.0.1" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIP_TrustProxyXFF(t *testing.T) {
	t.Setenv("TRUST_PROXY", "true")

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Forwarded-For", "8.8.8.8, 1.1.1.1")

	if got := ClientIP(r); got != "8.8.8.8" {
		t.Fatalf("got %q", got)
	}
}

func TestClientIP_UnknownWithoutTrust(t *testing.T) {
	if err := os.Unsetenv("TRUST_PROXY"); err != nil {
		t.Fatalf("unsetenv error: %v", err)
	}

	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Forwarded-For", "8.8.8.8")

	if got := ClientIP(r); got != "unknown" {
		t.Fatalf("got %q", got)
	}
}
