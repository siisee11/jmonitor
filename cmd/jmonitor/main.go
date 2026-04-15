package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"github.com/dev/jmonitor/internal/app"
	"github.com/dev/jmonitor/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	defer application.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go application.RunPoller(ctx)

	log.Printf("listening on %s", cfg.HTTPAddr)
	if err := application.RunHTTP(ctx); err != nil && err != http.ErrServerClosed {
		log.Fatalf("run http: %v", err)
	}
}
