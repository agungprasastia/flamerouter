package stream

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteSSEHeaders(rec)

	res := rec.Result()
	defer res.Body.Close() //nolint:errcheck // best effort close in test

	tests := []struct {
		header string
		want   string
	}{
		{"Content-Type", "text/event-stream"},
		{"Cache-Control", "no-cache"},
		{"Connection", "keep-alive"},
	}

	for _, tt := range tests {
		got := res.Header.Get(tt.header)
		if got != tt.want {
			t.Errorf("Header %s = %q; want %q", tt.header, got, tt.want)
		}
	}
}

type dummyFlusherResponseRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (d *dummyFlusherResponseRecorder) Flush() {
	d.flushed = true
}

type errReader struct {
	err error
}

func (e *errReader) Read(_ []byte) (n int, err error) {
	return 0, e.err
}

type errWriter struct {
	err error
}

func (e *errWriter) Header() http.Header {
	return http.Header{}
}

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, e.err
}

func (e *errWriter) WriteHeader(_ int) {}

func TestPipe(t *testing.T) {
	t.Run("successful pipe without flusher", func(t *testing.T) {
		rec := httptest.NewRecorder()
		src := strings.NewReader("data: hello\n\n")

		err := Pipe(rec, src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if rec.Body.String() != "data: hello\n\n" {
			t.Fatalf("got body %q, want %q", rec.Body.String(), "data: hello\n\n")
		}
	})

	t.Run("successful pipe with flusher", func(t *testing.T) {
		rec := &dummyFlusherResponseRecorder{
			ResponseRecorder: httptest.NewRecorder(),
			flushed:          false,
		}
		src := strings.NewReader("data: test flusher\n\n")

		err := Pipe(rec, src)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !rec.flushed {
			t.Errorf("expected flusher to be called")
		}

		if rec.Body.String() != "data: test flusher\n\n" {
			t.Fatalf("got body %q, want %q", rec.Body.String(), "data: test flusher\n\n")
		}
	})

	t.Run("reader error", func(t *testing.T) {
		rec := httptest.NewRecorder()
		expectedErr := errors.New("read error")
		src := &errReader{err: expectedErr}

		err := Pipe(rec, src)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("got error %v, want %v", err, expectedErr)
		}
	})

	t.Run("writer error", func(t *testing.T) {
		expectedErr := errors.New("write error")
		dst := &errWriter{err: expectedErr}
		src := strings.NewReader("some data")

		err := Pipe(dst, src)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("got error %v, want %v", err, expectedErr)
		}
	})
}

func TestPipeWithHeartbeat(t *testing.T) {
	t.Run("successful pipe data", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		rec := &dummyFlusherResponseRecorder{
			ResponseRecorder: httptest.NewRecorder(),
			flushed:          false,
		}
		src := strings.NewReader("data: chunk1\n\n")

		err := PipeWithHeartbeat(ctx, rec, src, 50*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(rec.Body.String(), "data: chunk1\n\n") {
			t.Fatalf("got body %q, want containing %q", rec.Body.String(), "data: chunk1\n\n")
		}
	})

	t.Run("heartbeat fired on idle", func(t *testing.T) {
		pr, pw := io.Pipe()

		defer pr.Close() //nolint:errcheck // test cleanup
		defer pw.Close() //nolint:errcheck // test cleanup

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		rec := httptest.NewRecorder()

		// Read in goroutine to let heartbeat tick
		errCh := make(chan error, 1)
		go func() {
			errCh <- PipeWithHeartbeat(ctx, rec, pr, 20*time.Millisecond)
		}()

		time.Sleep(70 * time.Millisecond)
		_ = pw.Close() //nolint:errcheck // best effort close

		err := <-errCh
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(rec.Body.String(), ": keepalive\n\n") {
			t.Fatalf("expected keepalive in output, got %q", rec.Body.String())
		}
	})

	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		rec := httptest.NewRecorder()
		pr, pw := io.Pipe()

		defer pr.Close() //nolint:errcheck // test cleanup
		defer pw.Close() //nolint:errcheck // test cleanup

		err := PipeWithHeartbeat(ctx, rec, pr, 100*time.Millisecond)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got error %v, want %v", err, context.Canceled)
		}
	})

	t.Run("heartbeat write error", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		expectedErr := errors.New("heartbeat write error")
		dst := &errWriter{err: expectedErr}
		pr, pw := io.Pipe()

		defer pr.Close() //nolint:errcheck // test cleanup
		defer pw.Close() //nolint:errcheck // test cleanup

		err := PipeWithHeartbeat(ctx, dst, pr, 10*time.Millisecond)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("got error %v, want %v", err, expectedErr)
		}
	})

	t.Run("reader error in PipeWithHeartbeat", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		expectedErr := errors.New("stream read error")
		rec := httptest.NewRecorder()
		src := &errReader{err: expectedErr}

		err := PipeWithHeartbeat(ctx, rec, src, 100*time.Millisecond)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("got error %v, want %v", err, expectedErr)
		}
	})

	t.Run("writer error on stream chunk", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		expectedErr := errors.New("chunk write error")
		dst := &errWriter{err: expectedErr}
		src := bytes.NewReader([]byte("chunk data"))

		err := PipeWithHeartbeat(ctx, dst, src, 100*time.Millisecond)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("got error %v, want %v", err, expectedErr)
		}
	})
}
