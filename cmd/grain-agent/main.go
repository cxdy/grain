package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cxdy/grain/internal/agent"
)

func main() {
	listen := flag.String("listen", envOr("GRAIN_AGENT_LISTEN", agent.DefaultListen), "listen address (e.g. :7475 or 0.0.0.0:7475)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(agent.Version)
		os.Exit(0)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	srv := agent.NewServer(*listen, log)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	case sig := <-sigCh:
		log.Info("shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("shutdown error", "err", err)
			os.Exit(1)
		}
		// Drain Serve result.
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
