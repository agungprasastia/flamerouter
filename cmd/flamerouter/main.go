package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/gateway"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/ops"
	"flamerouter/internal/store"

	// Self-register translators + specialized executors
	_ "flamerouter/internal/translator/request"
	_ "flamerouter/internal/translator/response"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("flamerouter 0.1.0-dev")
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: flamerouter <serve|version>")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	keys := auth.New(cfg.APIKeySecret)
	exec := executor.NewDefault(&http.Client{Timeout: 5 * time.Minute})

	// optional console ring buffer sink (translator playground)
	log.SetOutput(io.MultiWriter(os.Stderr, ops.Writer{Log: ops.DefaultConsole}))

	h := gateway.New(cfg, st, keys, exec)
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{Addr: addr, Handler: h}
	gateway.SetHTTPServer(srv)
	log.Printf("FlameRouter listening on %s (data %s)", addr, cfg.DataDir)
	log.Fatal(srv.ListenAndServe())
}
