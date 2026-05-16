package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/sudebaker/acb-go/internal/api"
	"github.com/sudebaker/acb-go/internal/config"
	"github.com/sudebaker/acb-go/internal/db"
	"github.com/sudebaker/acb-go/internal/dispatcher"
	acbredis "github.com/sudebaker/acb-go/internal/redis"
	"github.com/sudebaker/acb-go/internal/rustfs"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatal(err)
	}
	defer database.Close()

	if err := db.RunMigrations(database); err != nil {
		log.Fatal(err)
	}

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPass,
		DB:       0,
	})
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
		log.Printf("warning: failed to ensure rustfs bucket: %v", err)
	}

	// Dispatcher: webhook push + retry queue
	disp := dispatcher.NewDispatcher(agentRepo, taskRepo, rdb)
	disp.Start()
	defer disp.Stop()

	r := api.NewRouter(taskRepo, gateRepo, agentRepo, pub, rustfsClient, disp)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("ACB listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}