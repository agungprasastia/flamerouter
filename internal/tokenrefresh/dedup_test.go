package tokenrefresh_test

import (
	"context"
	"errors"
	"flamerouter/internal/tokenrefresh"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockRefresher struct {
	resultFn     func() (*tokenrefresh.RefreshResult, error)
	refreshCount int64
	delay        time.Duration
	mu           sync.Mutex
}

func (m *mockRefresher) Refresh(ctx context.Context, refreshToken string) (*tokenrefresh.RefreshResult, error) {
	atomic.AddInt64(&m.refreshCount, 1)

	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.resultFn != nil {
		return m.resultFn()
	}

	return &tokenrefresh.RefreshResult{
		AccessToken:  "new-access-token",
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(1 * time.Hour),
	}, nil
}

func TestConcurrentCallsSingleFlight(t *testing.T) {
	rm := tokenrefresh.NewRefreshManager()
	mock := &mockRefresher{
		delay: 50 * time.Millisecond,
	}
	rm.Register("testprovider", mock)

	const concurrency = 10

	var wg sync.WaitGroup

	wg.Add(concurrency)

	results := make([]*tokenrefresh.RefreshResult, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i

		go func() {
			defer wg.Done()

			res, err := rm.Refresh(context.Background(), "testprovider", "refresh-token-123")
			results[idx] = res
			errs[idx] = err
		}()
	}

	wg.Wait()

	if count := atomic.LoadInt64(&mock.refreshCount); count != 1 {
		t.Fatalf("expected exactly 1 upstream refresh call, got %d", count)
	}

	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("call %d failed: %v", i, errs[i])
		}

		if results[i] == nil || results[i].AccessToken != "new-access-token" {
			t.Fatalf("call %d unexpected result: %+v", i, results[i])
		}
	}
}

func TestCacheHitWithinTTL(t *testing.T) {
	rm := tokenrefresh.NewRefreshManager()
	mock := &mockRefresher{}
	rm.Register("testprovider", mock)

	// First call
	res1, err := rm.Refresh(context.Background(), "testprovider", "token-ttl")
	if err != nil {
		t.Fatalf("first refresh failed: %v", err)
	}

	if res1.AccessToken != "new-access-token" {
		t.Fatalf("expected new-access-token, got %s", res1.AccessToken)
	}

	if count := atomic.LoadInt64(&mock.refreshCount); count != 1 {
		t.Fatalf("expected 1 call, got %d", count)
	}

	// Immediate second call should hit cache
	res2, err := rm.Refresh(context.Background(), "testprovider", "token-ttl")
	if err != nil {
		t.Fatalf("second refresh failed: %v", err)
	}

	if res2.AccessToken != "new-access-token" {
		t.Fatalf("expected new-access-token, got %s", res2.AccessToken)
	}

	if count := atomic.LoadInt64(&mock.refreshCount); count != 1 {
		t.Fatalf("expected cache hit (count=1), got %d", count)
	}
}

func TestCacheExpiryTriggersNewRefresh(t *testing.T) {
	rm := tokenrefresh.NewRefreshManagerWithTTL(50 * time.Millisecond)
	mock := &mockRefresher{}
	rm.Register("testprovider", mock)

	// Call 1
	_, err := rm.Refresh(context.Background(), "testprovider", "token-expiry")
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	if count := atomic.LoadInt64(&mock.refreshCount); count != 1 {
		t.Fatalf("expected 1 call, got %d", count)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Call 2 after expiry
	_, err = rm.Refresh(context.Background(), "testprovider", "token-expiry")
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if count := atomic.LoadInt64(&mock.refreshCount); count != 2 {
		t.Fatalf("expected 2 calls after expiry, got %d", count)
	}
}

func TestConcurrentCallsFailedRefreshDoesNotCacheError(t *testing.T) {
	rm := tokenrefresh.NewRefreshManager()

	var attempts int64

	mock := &mockRefresher{
		resultFn: func() (*tokenrefresh.RefreshResult, error) {
			att := atomic.AddInt64(&attempts, 1)
			if att <= 3 { // remember refreshWithRetry has retries
				return nil, errors.New("temporary error")
			}
			return &tokenrefresh.RefreshResult{
				AccessToken: "recovered-token",
			}, nil
		},
	}
	rm.Register("testprovider", mock)

	// Call 1 should fail
	_, err := rm.Refresh(context.Background(), "testprovider", "err-token")
	if err == nil {
		t.Fatalf("expected error on call 1")
	}

	// Call 2 should retry fresh upstream since failure isn't cached
	res, err := rm.Refresh(context.Background(), "testprovider", "err-token")
	if err != nil {
		t.Fatalf("call 2 failed: %v", err)
	}

	if res.AccessToken != "recovered-token" {
		t.Fatalf("expected recovered-token, got %s", res.AccessToken)
	}
}
