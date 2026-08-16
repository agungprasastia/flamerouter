package testutil

import (
	"context"
	"errors"
	"flamerouter/internal/opensse/executor"
	"io"
	"net/http"
	"testing"
)

func verifyHeaders(t *testing.T, result *executor.Result, responseHeader http.Header) {
	t.Helper()

	if result.StatusCode != 201 {
		t.Fatalf("status = %d", result.StatusCode)
	}

	if got := result.Header.Get("X-Test"); got != "source" {
		t.Fatalf("header = %q", got)
	}

	result.Header.Set("X-Test", "changed")

	if got := responseHeader.Get("X-Test"); got != "source" {
		t.Fatalf("source header = %q", got)
	}
}

func verifyCalls(t *testing.T, calls []Call) {
	t.Helper()

	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}

	call := calls[0]
	if call.Credentials.APIKey != "k" {
		t.Fatalf("api key = %q", call.Credentials.APIKey)
	}

	if call.Credentials.ProviderSpecificData["region"] != "test" {
		t.Fatalf("provider data = %v", call.Credentials.ProviderSpecificData)
	}

	if call.Model != "p/model" {
		t.Fatalf("model = %q", call.Model)
	}

	if got := string(call.Body); got != `{"input":"test"}` {
		t.Fatalf("request body = %q", got)
	}

	if !call.Stream {
		t.Fatal("stream = false")
	}
}

func TestFakeExecutorCapturesCallAndReturnsResponse(t *testing.T) {
	responseHeader := http.Header{"X-Test": {"source"}}
	fake := NewFakeExecutor(Response{
		StatusCode: 201,
		Header:     responseHeader,
		Body:       []byte(`{"ok":true}`),
		StreamBody: nil,
	})
	requestBody := []byte(`{"input":"test"}`)

	result, err := fake.Execute(context.Background(), executor.Credentials{
		APIKey:               "k",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProjectID:            "",
		ProviderSpecificData: map[string]any{"region": "test"},
	}, "p/model", requestBody, true)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := result.Body.Close(); closeErr != nil {
			t.Logf("close error: %v", closeErr)
		}
	})

	verifyHeaders(t, result, responseHeader)

	responseBody, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(responseBody); got != `{"ok":true}` {
		t.Fatalf("body = %q", got)
	}

	verifyCalls(t, fake.Calls())
}

func TestFakeExecutorReturnsQueuedErrorBeforeResponse(t *testing.T) {
	queuedError := errors.New("transport failed")
	fake := NewFakeExecutor(Response{
		StatusCode: 200,
		Header:     nil,
		Body:       nil,
		StreamBody: []byte("stream"),
	})
	fake.QueueError(queuedError)

	emptyCreds := executor.Credentials{
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProjectID:            "",
		ProviderSpecificData: nil,
	}

	result, err := fake.Execute(context.Background(), emptyCreds, "p/model", nil, true)
	if !errors.Is(err, queuedError) {
		t.Fatalf("error = %v", err)
	}

	if result != nil {
		t.Fatalf("result = %+v", result)
	}

	result, err = fake.Execute(context.Background(), emptyCreds, "p/model", nil, true)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := result.Body.Close(); closeErr != nil {
			t.Logf("close error: %v", closeErr)
		}
	})

	responseBody, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}

	if got := string(responseBody); got != "stream" {
		t.Fatalf("stream body = %q", got)
	}
}
