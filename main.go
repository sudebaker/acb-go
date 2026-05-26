package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sudebaker/acb-go/internal/api"
	"github.com/sudebaker/acb-go/internal/config"
	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/dispatcher"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/sudebaker/acb-go/internal/rustfs"
	"github.com/sudebaker/acb-go/internal/timeout"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	switch cfg.LogLevel {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	database, err := db.Open(cfg.PGHost, cfg.PGPort, cfg.PGUser, cfg.PGPassword, cfg.PGDatabase)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open database")
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
		DB:       0,
	})
	if cfg.RedisPass == "" {
		log.Warn().Msg("Redis password (ACB_REDIS_PASS) is not set — using unauthenticated connection")
	}
	defer rdb.Close()

	taskRepo := db.NewTaskRepo(database)
	gateRepo := db.NewGateRepo(database)
	agentRepo := db.NewAgentRepo(database)
	eventRepo := db.NewTaskEventRepo(database)
	taskRepo.WithEventRepo(eventRepo)
	pub := acbredis.NewPublisher(rdb)

	rustfsClient := rustfs.NewClient(
		cfg.RustFSEndpoint, cfg.RustFSRegion,
		cfg.RustFSAccessKey, cfg.RustFSSecretKey,
		cfg.RustFSBucket,
	)
	if err := rustfsClient.EnsureBucket(context.Background()); err != nil {
		log.Warn().Err(err).Msg("failed to ensure rustfs bucket")
	}

	// Dispatcher: webhook push + retry queue
	disp := dispatcher.NewDispatcher(agentRepo, taskRepo, rdb)
	disp.Start()
	defer disp.Stop()

	// Timeout service: cancels stale pending tasks, in-progress tasks with no heartbeat,
	// and releases tasks from agents that stopped heartbeating.
	timeoutSvc := timeout.NewTimeoutService(
		taskRepo,
		agentRepo,
		cfg.PendingTimeoutMin,
		cfg.TaskTimeoutMin,
		cfg.AgentStaleMin,
		time.Duration(cfg.PendingTimeoutCheckSec)*time.Second,
	)
	timeoutSvc.Start()
	defer timeoutSvc.Stop()

	r := api.NewRouter(taskRepo, gateRepo, agentRepo, pub, rustfsClient, disp, cfg, database, rdb)

	srv := &http.Server{Addr: fmt.Sprintf(":%d", cfg.Port), Handler: r}

	go func() {
		log.Info().Int("port", cfg.Port).Msg("ACB listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("shutting down gracefully")

	disp.Stop()
	timeoutSvc.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("server forced to shutdown")
	}

	rdb.Close()
	database.Close()
	log.Info().Msg("server stopped")
}