package stream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockFlushingWriter struct {
	*httptest.ResponseRecorder
	writeErr   error
	flushCount int
}

func newMockFlushingWriter() *mockFlushingWriter {
	return &mockFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeErr:         nil,
		flushCount:       0,
	}
}

func (m *mockFlushingWriter) Flush() {
	m.flushCount++
}

func (m *mockFlushingWriter) Write(b []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}

	return m.ResponseRecorder.Write(b)
}

type mockNonFlushingWriter struct {
	*httptest.ResponseRecorder
	writeErr error
}

func newMockNonFlushingWriter() *mockNonFlushingWriter {
	return &mockNonFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeErr:         nil,
	}
}

func (m *mockNonFlushingWriter) Write(b []byte) (int, error) {
	if m.writeErr != nil {
		return 0, m.writeErr
	}

	return m.ResponseRecorder.Write(b)
}

type chunkedReader struct {
	chunks [][]byte
	idx    int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.idx >= len(r.chunks) {
		return 0, io.EOF
	}

	chunk := r.chunks[r.idx]
	r.idx++
	n := copy(p, chunk)

	return n, nil
}

type errReader struct {
	err  error
	data []byte
	read bool
}

func (r *errReader) Read(p []byte) (int, error) {
	if !r.read && len(r.data) > 0 {
		r.read = true
		n := copy(p, r.data)

		return n, nil
	}

	return 0, r.err
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

type trackingCloserReader struct {
	io.Reader
	mu     sync.Mutex
	closed bool
}

func (t *trackingCloserReader) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.closed = true

	return nil
}

func (t *trackingCloserReader) IsClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.closed
}

type blockingPipeReader struct {
	ch     chan []byte
	closed chan struct{}
	once   sync.Once
}

func newBlockingPipeReader() *blockingPipeReader {
	return &blockingPipeReader{
		ch:     make(chan []byte, 10),
		closed: make(chan struct{}),
		once:   sync.Once{},
	}
}

func (r *blockingPipeReader) Read(p []byte) (int, error) {
	select {
	case data, ok := <-r.ch:
		if !ok {
			return 0, io.EOF
		}

		n := copy(p, data)

		return n, nil
	case <-r.closed:
		return 0, io.EOF
	}
}

func (r *blockingPipeReader) Close() error {
	r.once.Do(func() {
		close(r.closed)
	})

	return nil
}

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

func TestPipe_SuccessWithFlusher(t *testing.T) {
	w := newMockFlushingWriter()
	src := &chunkedReader{
		chunks: [][]byte{
			[]byte("data: hello\n\n"),
			[]byte("data: world\n\n"),
		},
		idx: 0,
	}

	err := Pipe(w, src)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedBody := "data: hello\n\ndata: world\n\n"

	if got := w.Body.String(); got != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, got)
	}

	if w.flushCount != 2 {
		t.Errorf("expected flush count 2, got %d", w.flushCount)
	}
}

func TestPipe_SuccessWithoutFlusher(t *testing.T) {
	w := newMockNonFlushingWriter()
	src := &chunkedReader{
		chunks: [][]byte{
			[]byte("chunk 1"),
			[]byte("chunk 2"),
		},
		idx: 0,
	}

	err := Pipe(w, src)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedBody := "chunk 1chunk 2"

	if got := w.Body.String(); got != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, got)
	}
}

func TestPipe_LargeDataBufferLimit(t *testing.T) {
	w := newMockFlushingWriter()
	largeData := bytes.Repeat([]byte("A"), 70*1024)
	src := bytes.NewReader(largeData)

	err := Pipe(w, src)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if w.Body.Len() != len(largeData) {
		t.Errorf("expected body size %d, got %d", len(largeData), w.Body.Len())
	}

	if w.flushCount < 3 {
		t.Errorf("expected at least 3 flushes for large payload, got %d", w.flushCount)
	}
}

func TestPipe_EmptyReader(t *testing.T) {
	w := newMockFlushingWriter()
	src := bytes.NewReader([]byte{})

	err := Pipe(w, src)
	if err != nil {
		t.Fatalf("expected no error on empty reader, got %v", err)
	}

	if w.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", w.Body.String())
	}

	if w.flushCount != 0 {
		t.Errorf("expected 0 flushes, got %d", w.flushCount)
	}
}

func TestPipe_WriteError(t *testing.T) {
	writeErr := errors.New("write failure")
	w := &mockFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeErr:         writeErr,
		flushCount:       0,
	}
	src := bytes.NewReader([]byte("some data"))

	err := Pipe(w, src)
	if !errors.Is(err, writeErr) {
		t.Errorf("expected write error %v, got %v", writeErr, err)
	}
}

func TestPipe_ReadError(t *testing.T) {
	w := newMockFlushingWriter()
	customErr := errors.New("read failed")
	src := &errReader{
		err:  customErr,
		data: []byte("partial data"),
		read: false,
	}

	err := Pipe(w, src)
	if !errors.Is(err, customErr) {
		t.Errorf("expected error %v, got %v", customErr, err)
	}

	if got := w.Body.String(); got != "partial data" {
		t.Errorf("expected partial data written, got %q", got)
	}
}

func TestPipeWithHeartbeat_Success(t *testing.T) {
	w := newMockFlushingWriter()
	src := &chunkedReader{
		chunks: [][]byte{
			[]byte("data: 1\n\n"),
			[]byte("data: 2\n\n"),
		},
		idx: 0,
	}

	ctx := context.Background()

	err := PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedBody := "data: 1\n\ndata: 2\n\n"

	if got := w.Body.String(); got != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, got)
	}

	if w.flushCount != 2 {
		t.Errorf("expected flush count 2, got %d", w.flushCount)
	}
}

func TestPipeWithHeartbeat_SendsHeartbeat(t *testing.T) {
	w := newMockFlushingWriter()
	blockingReader := newBlockingPipeReader()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		errCh <- PipeWithHeartbeat(ctx, w, blockingReader, 20*time.Millisecond)
	}()

	time.Sleep(60 * time.Millisecond)

	blockingReader.ch <- []byte("data: message\n\n")

	time.Sleep(10 * time.Millisecond)
	close(blockingReader.ch)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for PipeWithHeartbeat to return")
	}

	body := w.Body.String()

	if !bytes.Contains(w.Body.Bytes(), []byte(": keepalive\n\n")) {
		t.Errorf("expected heartbeat in body, got %q", body)
	}

	if !bytes.Contains(w.Body.Bytes(), []byte("data: message\n\n")) {
		t.Errorf("expected data in body, got %q", body)
	}
}

func TestPipeWithHeartbeat_ContextCancelled(t *testing.T) {
	w := newMockFlushingWriter()
	blockingReader := newBlockingPipeReader()

	defer func() {
		if clErr := blockingReader.Close(); clErr != nil {
			t.Errorf("close error: %v", clErr)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- PipeWithHeartbeat(ctx, w, blockingReader, 100*time.Millisecond)
	}()

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for context cancellation response")
	}
}

func TestPipeWithHeartbeat_ClosesCloserReader(t *testing.T) {
	w := newMockFlushingWriter()
	baseReader := bytes.NewReader([]byte("hello"))
	tracker := &trackingCloserReader{
		Reader: baseReader,
		mu:     sync.Mutex{},
		closed: false,
	}

	ctx := context.Background()

	err := PipeWithHeartbeat(ctx, w, tracker, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	closed := false

	for i := 0; i < 50; i++ {
		if tracker.IsClosed() {
			closed = true

			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if !closed {
		t.Errorf("expected reader to be closed on exit")
	}
}

func TestPipeWithHeartbeat_HeartbeatWriteError(t *testing.T) {
	writeErr := errors.New("write failed on heartbeat")
	w := &mockFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeErr:         writeErr,
		flushCount:       0,
	}
	blockingReader := newBlockingPipeReader()

	defer func() {
		if clErr := blockingReader.Close(); clErr != nil {
			t.Errorf("close error: %v", clErr)
		}
	}()

	ctx := context.Background()
	errCh := make(chan error, 1)

	go func() {
		errCh <- PipeWithHeartbeat(ctx, w, blockingReader, 10*time.Millisecond)
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, writeErr) {
			t.Errorf("expected error %v, got %v", writeErr, err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for heartbeat write error")
	}
}

func TestPipeWithHeartbeat_DefaultInterval(t *testing.T) {
	w := newMockFlushingWriter()
	src := bytes.NewReader([]byte("test"))

	ctx := context.Background()

	err := PipeWithHeartbeat(ctx, w, src, 0)
	if err != nil {
		t.Fatalf("expected no error with default interval, got %v", err)
	}

	got := w.Body.String()
	if got != "test" {
		t.Errorf("expected body %q, got %q", "test", got)
	}
}

func TestPipeWithHeartbeat_ReadError(t *testing.T) {
	w := newMockFlushingWriter()
	readErr := fmt.Errorf("stream interrupted")
	src := &errReader{
		err:  readErr,
		data: []byte("partial"),
		read: false,
	}

	ctx := context.Background()

	err := PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if !errors.Is(err, readErr) {
		t.Errorf("expected error %v, got %v", readErr, err)
	}
}

func TestPipeWithHeartbeat_WithoutFlusher(t *testing.T) {
	w := newMockNonFlushingWriter()
	src := bytes.NewReader([]byte("no flusher test"))

	ctx := context.Background()

	err := PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	got := w.Body.String()
	if got != "no flusher test" {
		t.Errorf("expected body %q, got %q", "no flusher test", got)
	}
}

func TestPipeWithHeartbeat_PipeErrorOnChunk(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	expectedErr := errors.New("chunk write error")
	dst := &errWriter{err: expectedErr}
	src := strings.NewReader("chunk data")

	err := PipeWithHeartbeat(ctx, dst, src, 100*time.Millisecond)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("got error %v, want %v", err, expectedErr)
	}
}
