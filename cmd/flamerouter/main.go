// Package main implements the flamerouter entrypoint command.
package main

import (
	"errors"
	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/gateway"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/ops"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	// Self-register translators + specialized executors.
	_ "flamerouter/internal/translator/request"
	_ "flamerouter/internal/translator/response"
)

func runServer() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}

	defer func() {
		if errClose := st.Close(); errClose != nil {
			log.Printf("store close: %v", errClose)
		}
	}()

	keys := auth.New(cfg.APIKeySecret)
	exec := executor.NewDefault(&http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       5 * time.Minute,
	})

	// optional console ring buffer sink (translator playground)
	log.SetOutput(io.MultiWriter(os.Stderr, ops.Writer{Log: ops.DefaultConsole}))

	h := gateway.New(cfg, st, keys, exec)
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:                         addr,
		Handler:                      h,
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  30 * time.Second,
		ReadHeaderTimeout:            30 * time.Second,
		WriteTimeout:                 0,
		IdleTimeout:                  120 * time.Second,
		MaxHeaderBytes:               0,
		TLSNextProto:                 nil,
		ConnState:                    nil,
		ErrorLog:                     nil,
		BaseContext:                  nil,
		ConnContext:                  nil,
		HTTP2:                        nil,
		Protocols:                    nil,
	}

	gateway.SetHTTPServer(srv)
	log.Printf("FlameRouter listening on %s (data %s)", addr, cfg.DataDir)

	if errServe := srv.ListenAndServe(); errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", errServe)
	}

	return nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("flamerouter 0.1.0-dev")
		return
	}

	if len(os.Args) < 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, "usage: flamerouter <serve|version>")
		os.Exit(2)
	}

	if err := runServer(); err != nil {
		log.Printf("fatal: %v", err)
		os.Exit(1)
	}
}
