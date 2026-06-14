package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"polyquant/demo-go-tunnel/internal/server"
)

func main() {
	cfg := server.LoadConfigFromEnv()

	logger := log.New(os.Stdout, "[tunnel-server] ", log.LstdFlags)
	store, err := server.NewAgentStoreFromEnv(logger)
	if err != nil {
		logger.Fatalf("failed to init agent store: %v", err)
	}

	relaySrv := server.NewRelayServer(cfg.RelayAddr, store, cfg.SharedToken, logger)
	httpSrv := server.NewHTTPServer(cfg, store, relaySrv, cfg.SharedToken, logger)
	socksSrv := server.NewSocksEntryServer(cfg.SocksAddr, relaySrv, store, cfg.SharedToken, logger)
	httpSrv.AttachSocksServer(socksSrv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 3)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
	go func() {
		if err := relaySrv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
	go func() {
		if err := socksSrv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Printf("shutdown signal received")
	case err := <-errCh:
		logger.Fatalf("server startup failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("http shutdown failed: %v", err)
	}
	if err := socksSrv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("socks shutdown failed: %v", err)
	}
	if err := relaySrv.Shutdown(shutdownCtx); err != nil {
		logger.Printf("relay shutdown failed: %v", err)
	}
	logger.Printf("shutdown complete")
}
