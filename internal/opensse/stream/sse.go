package stream

import (
	"io"
	"net/http"
)

func WriteSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
}

func Pipe(dst http.ResponseWriter, src io.Reader) error {
	flusher, _ := dst.(http.Flusher)
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
