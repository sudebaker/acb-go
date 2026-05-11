package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/amphora/acb/internal/api"
	"github.com/amphora/acb/internal/config"
	"github.com/amphora/acb/internal/db"
	acbredis "github.com/amphora/acb/internal/redis"
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
	pub := acbredis.NewPublisher(rdb)

	r := api.NewRouter(taskRepo, gateRepo, agentRepo, pub)

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("ACB listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
