// Package stream provides server-sent events (SSE) streaming utilities.
package stream

import (
	"io"
	"net/http"
)

// WriteSSEHeaders writes the standard headers required for Server-Sent Events streams.
func WriteSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
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
