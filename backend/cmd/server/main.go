package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcp-rag-demo/backend/internal/app"
	"mcp-rag-demo/backend/internal/config"
	"mcp-rag-demo/backend/internal/db"
	"mcp-rag-demo/backend/internal/httpapi"
	"mcp-rag-demo/backend/internal/llm"
	"mcp-rag-demo/backend/internal/mcpserver"
	"mcp-rag-demo/backend/internal/seed"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	client := llm.New(cfg.OpenAIKey, cfg.OpenAIBaseURL, cfg.OpenAIChatModel, cfg.OpenAIEmbedModel)
	if !client.Enabled() {
		slog.Warn("OPENAI_API_KEY is not set: indexing uses full-text search only, Answers are disabled")
	}
	a := app.New(pool, cfg, client)
	a.RunIndexer(ctx, 2)

	if cfg.Seed {
		if err := seed.Run(ctx, a); err != nil {
			slog.Error("seed", "err", err)
		}
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.Handler(a, mcpserver.Handler(a)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("listening", "addr", cfg.HTTPAddr, "mcp", "/mcp")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http", "err", err)
		os.Exit(1)
	}
}

// connect retries while Postgres is starting up.
func connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	var err error
	for attempt := 0; attempt < 30; attempt++ {
		var pool *pgxpool.Pool
		if pool, err = db.Open(ctx, url); err == nil {
			return pool, nil
		}
		slog.Info("waiting for database", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, err
}
