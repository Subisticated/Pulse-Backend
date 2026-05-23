package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pulse-backend/internal/config"
	"pulse-backend/internal/routes"
	"pulse-backend/internal/services"
	"pulse-backend/internal/wsocket"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/mongo"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339})
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	log.Info().Msg("🚀 Starting Pulse Backend...")

	cfg := config.LoadConfig()

	var db *mongo.Database
	client, db, err := config.ConnectDB(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("MongoDB connection failed")
	}

	// Ensure collection indexes (idempotent)
	services.EnsureIndexes(context.Background(), db)

	// WebSocket hub
	hub := wsocket.NewHub()
	go hub.Run()

	// Router (sets gin.ReleaseMode internally)
	router := routes.SetupRouter(db, hub)

	// ── HTTP Server ───────────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background goroutine
	go func() {
		log.Info().Str("addr", addr).Msg("Pulse Backend listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server error")
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // block until CTRL+C or kill signal

	log.Info().Msg("🛑 Shutdown signal received — draining...")

	// 1. Stop accepting new HTTP requests
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error().Err(err).Msg("HTTP server shutdown error")
	}

	// 2. Drain the log queue — flush all pending batches to MongoDB
	//    The queue reference lives inside routes; access it via the ingestion service.
	//    We use a package-level accessor exposed by routes.
	if q := routes.GetQueue(); q != nil {
		q.Shutdown(60 * time.Second) // allow up to 60s for full queue drain
	}

	// 3. Disconnect MongoDB
	disconnectCtx, disconnectCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer disconnectCancel()
	if err := client.Disconnect(disconnectCtx); err != nil {
		log.Error().Err(err).Msg("MongoDB disconnect error")
	}

	log.Info().Msg("✅ Pulse Backend shut down cleanly")
}
