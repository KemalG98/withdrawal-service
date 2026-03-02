package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/KemalG98/withdrawal-service/internal/handler"
	"github.com/KemalG98/withdrawal-service/internal/middleware"
	"github.com/KemalG98/withdrawal-service/internal/repository"
	"github.com/KemalG98/withdrawal-service/internal/service"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	dbURL := mustEnv("DATABASE_URL")
	token := mustEnv("BEARER_TOKEN")
	port := getEnv("PORT", "8080")

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("connect to db", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	for i := 0; i < 10; i++ {
		if err = pool.Ping(ctx); err == nil {
			break
		}
		logger.Warn("waiting for db...", "attempt", i+1)
		time.Sleep(time.Second)
	}
	if err != nil {
		logger.Error("db not available", "err", err)
		os.Exit(1)
	}

	repo := repository.New(pool)
	svc := service.New(repo)
	h := handler.New(svc, logger)

	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.BearerAuth(token))

	r.Post("/v1/withdrawals", h.CreateWithdrawal)
	r.Get("/v1/withdrawals/{id}", h.GetWithdrawal)
	r.Post("/v1/withdrawals/{id}/confirm", h.ConfirmWithdrawal)

	logger.Info("server starting", "port", port)
	if err = http.ListenAndServe(":"+port, r); err != nil {
		logger.Error("server error", "err", err)
		os.Exit(1)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("missing required env: " + key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
