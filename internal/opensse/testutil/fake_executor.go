// Package testutil provides mock executors and test fixtures for opensse packages.
package testutil

import (
	"bytes"
	"context"
	"flamerouter/internal/opensse/executor"
	"io"
	"maps"
	"net/http"
	"sync"
)

// Response represents a queued mock response in FakeExecutor.
type Response struct {
	Header     http.Header
	Body       []byte
	StreamBody []byte
	StatusCode int
}

// Call represents a recorded execution invocation in FakeExecutor.
type Call struct {
	Credentials executor.Credentials
	Model       string
	Body        []byte
	Stream      bool
}

// FakeExecutor is a mock implementation of executor.Executor for testing.
type FakeExecutor struct {
	responses []Response
	errors    []error
	calls     []Call
	mu        sync.Mutex
}

var _ executor.Executor = (*FakeExecutor)(nil)

// NewFakeExecutor constructs a FakeExecutor with the given initial queued responses.
func NewFakeExecutor(responses ...Response) *FakeExecutor {
	return &FakeExecutor{
		responses: append([]Response(nil), responses...),
		errors:    nil,
		calls:     nil,
		mu:        sync.Mutex{},
	}
}

// QueueResponse adds a response to the queue.
func (f *FakeExecutor) QueueResponse(response Response) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses = append(f.responses, response)
}

// QueueError adds an error to the error queue.
func (f *FakeExecutor) QueueError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, err)
}

// Calls returns a copy of all recorded calls.
func (f *FakeExecutor) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()

	calls := make([]Call, len(f.calls))
	for index, call := range f.calls {
		calls[index] = Call{
			Credentials: cloneCredentials(call.Credentials),
			Model:       call.Model,
			Body:        append([]byte(nil), call.Body...),
			Stream:      call.Stream,
		}
	}

	return calls
}

// Execute mocks execution by recording the call and returning queued response or error.
func (f *FakeExecutor) Execute(_ context.Context, credentials executor.Credentials, model string, body []byte, stream bool) (*executor.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, Call{
		Credentials: cloneCredentials(credentials),
		Model:       model,
		Body:        append([]byte(nil), body...),
		Stream:      stream,
	})
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]

		return nil, err
	}

	response := Response{
		Header:     nil,
		Body:       nil,
		StreamBody: nil,
		StatusCode: 0,
	}
	if len(f.responses) > 0 {
		response = f.responses[0]
		f.responses = f.responses[1:]
	}

	bodyBytes := response.Body
	if stream && response.StreamBody != nil {
		bodyBytes = response.StreamBody
	}

	return &executor.Result{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(bodyBytes)),
	}, nil
}

func cloneCredentials(credentials executor.Credentials) executor.Credentials {
	clone := credentials
	if credentials.ProviderSpecificData != nil {
		clone.ProviderSpecificData = make(map[string]any, len(credentials.ProviderSpecificData))
		maps.Copy(clone.ProviderSpecificData, credentials.ProviderSpecificData)
	}

	return clone
}
