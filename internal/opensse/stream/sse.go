// Package stream provides server-sent events (SSE) streaming utilities.
package stream

import (
	"context"
	"io"
	"net/http"
	"time"
)

// WriteSSEHeaders writes the standard headers required for Server-Sent Events streams.
func WriteSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

type readResult struct {
	err error
	buf []byte
	n   int
}

func startReaderRoutine(ctx context.Context, src io.Reader, resCh chan<- readResult) {
	defer func() {
		if closer, ok := src.(io.Closer); ok {
			_ = closer.Close() //nolint:errcheck // best-effort close on reader exit
		}
	}()

	for {
		buf := make([]byte, 32*1024)

		n, err := src.Read(buf)
		select {
		case <-ctx.Done():
			return
		case resCh <- readResult{err: err, buf: buf, n: n}:
		}

		if err != nil {
			return
		}
	}
}

func flushHeartbeat(dst http.ResponseWriter, flusher http.Flusher) error {
	if _, werr := dst.Write([]byte(": keepalive\n\n")); werr != nil {
		return werr
	}

	if flusher != nil {
		flusher.Flush()
	}

	return nil
}

func flushChunk(dst http.ResponseWriter, flusher http.Flusher, res readResult) error {
	if res.n > 0 {
		if _, werr := dst.Write(res.buf[:res.n]); werr != nil {
			return werr
		}

		if flusher != nil {
			flusher.Flush()
		}
	}

	if res.err != nil {
		if res.err == io.EOF {
			return nil
		}

		return res.err
	}

	return nil
}

func processStreamEvent(dst http.ResponseWriter, flusher http.Flusher, res readResult) (bool, error) {
	if err := flushChunk(dst, flusher, res); err != nil {
		return true, err
	}

	if res.err != nil && res.err == io.EOF {
		return true, nil
	}

	return res.err != nil, res.err
}

// PipeWithHeartbeat reads from src and writes to dst while flushing each chunk if supported,
// sending periodic SSE comment keep-alives during idle read windows.
func PipeWithHeartbeat(ctx context.Context, dst http.ResponseWriter, src io.Reader, interval time.Duration) error {
	flusher, ok := dst.(http.Flusher)
	if !ok {
		flusher = nil
	}

	if interval <= 0 {
		interval = 15 * time.Second
	}

	resCh := make(chan readResult, 1)

	go startReaderRoutine(ctx, src, resCh)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := flushHeartbeat(dst, flusher); err != nil {
				return err
			}
		case res := <-resCh:
			ticker.Reset(interval)

			done, err := processStreamEvent(dst, flusher, res)
			if done {
				return err
			}
		}
	}
}

// Pipe reads from src and writes to dst while flushing each chunk if supported.
func Pipe(dst http.ResponseWriter, src io.Reader) error {
	flusher, ok := dst.(http.Flusher)
	if !ok {
		flusher = nil
	}

	buf := make([]byte, 32*1024)

	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}

			if flusher != nil {
				flusher.Flush()
			}
		}

		if err != nil {
			if err == io.EOF {
				return nil
			}

			return err
		}
	}
}
