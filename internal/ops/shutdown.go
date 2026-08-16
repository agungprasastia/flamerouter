package ops

import (
	"context"
	"net/http"
	"time"
)

// Shutdown gracefully stops the server.
func Shutdown(srv *http.Server) error {
	if srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(ctx)
}
