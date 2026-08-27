package stream_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"flamerouter/internal/opensse/stream"
)

type mockFlushingWriter struct {
	*httptest.ResponseRecorder
	flushCount int
	writeErr   error
}

func newMockFlushingWriter() *mockFlushingWriter {
	return &mockFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
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
	data []byte
	err  error
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
}

func newBlockingPipeReader() *blockingPipeReader {
	return &blockingPipeReader{
		ch:     make(chan []byte, 10),
		closed: make(chan struct{}),
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
	select {
	case <-r.closed:
	default:
		close(r.closed)
	}
	return nil
}

func TestWriteSSEHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	stream.WriteSSEHeaders(rec)

	res := rec.Result()
	if got := res.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %q", got)
	}
	if got := res.Header.Get("Connection"); got != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %q", got)
	}
}

func TestPipe_SuccessWithFlusher(t *testing.T) {
	w := newMockFlushingWriter()
	src := &chunkedReader{
		chunks: [][]byte{
			[]byte("data: hello\n\n"),
			[]byte("data: world\n\n"),
		},
	}

	err := stream.Pipe(w, src)
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
	}

	err := stream.Pipe(w, src)
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
	// Create payload larger than 32KB buffer size (e.g. 70KB)
	largeData := bytes.Repeat([]byte("A"), 70*1024)
	src := bytes.NewReader(largeData)

	err := stream.Pipe(w, src)
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

	err := stream.Pipe(w, src)
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
	}
	src := bytes.NewReader([]byte("some data"))

	err := stream.Pipe(w, src)
	if !errors.Is(err, writeErr) {
		t.Errorf("expected write error %v, got %v", writeErr, err)
	}
}

func TestPipe_ReadError(t *testing.T) {
	w := newMockFlushingWriter()
	customErr := errors.New("read failed")
	src := &errReader{
		data: []byte("partial data"),
		err:  customErr,
	}

	err := stream.Pipe(w, src)
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
	}

	ctx := context.Background()
	err := stream.PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	expectedBody := "data: 1\n\ndata: 2\n\n"
	if got := w.Body.String(); got != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, got)
	}
	if w.flushCount != 2 {
		t.Errorf("expected 2 flushes, got %d", w.flushCount)
	}
}

func TestPipeWithHeartbeat_SendsHeartbeat(t *testing.T) {
	w := newMockFlushingWriter()
	blockingReader := newBlockingPipeReader()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- stream.PipeWithHeartbeat(ctx, w, blockingReader, 20*time.Millisecond)
	}()

	// Wait long enough for at least one heartbeat tick
	time.Sleep(60 * time.Millisecond)

	// Send data then close reader
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
	defer blockingReader.Close()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- stream.PipeWithHeartbeat(ctx, w, blockingReader, 100*time.Millisecond)
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
	tracker := &trackingCloserReader{Reader: baseReader}

	ctx := context.Background()
	err := stream.PipeWithHeartbeat(ctx, w, tracker, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !tracker.IsClosed() {
		t.Errorf("expected reader to be closed on exit")
	}
}

func TestPipeWithHeartbeat_HeartbeatWriteError(t *testing.T) {
	writeErr := errors.New("write failed on heartbeat")
	w := &mockFlushingWriter{
		ResponseRecorder: httptest.NewRecorder(),
		writeErr:         writeErr,
	}
	blockingReader := newBlockingPipeReader()
	defer blockingReader.Close()

	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- stream.PipeWithHeartbeat(ctx, w, blockingReader, 10*time.Millisecond)
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
	// Passing interval <= 0 should use default interval (15s) and not crash or panic
	err := stream.PipeWithHeartbeat(ctx, w, src, 0)
	if err != nil {
		t.Fatalf("expected no error with default interval, got %v", err)
	}
	if got := w.Body.String(); got != "test" {
		t.Errorf("expected body %q, got %q", "test", got)
	}
}

func TestPipeWithHeartbeat_ReadError(t *testing.T) {
	w := newMockFlushingWriter()
	readErr := fmt.Errorf("stream interrupted")
	src := &errReader{
		data: []byte("partial"),
		err:  readErr,
	}

	ctx := context.Background()
	err := stream.PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if !errors.Is(err, readErr) {
		t.Errorf("expected error %v, got %v", readErr, err)
	}
}

func TestPipeWithHeartbeat_WithoutFlusher(t *testing.T) {
	w := newMockNonFlushingWriter()
	src := bytes.NewReader([]byte("no flusher test"))

	ctx := context.Background()
	err := stream.PipeWithHeartbeat(ctx, w, src, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := w.Body.String(); got != "no flusher test" {
		t.Errorf("expected body %q, got %q", "no flusher test", got)
	}
}
