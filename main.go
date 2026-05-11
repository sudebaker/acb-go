package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/amphora/acb/internal/config"
)

func main() {
	cfg := config.Load()
	log.Printf("ACB starting on port %d", cfg.Port)

	// TODO: wire up handlers, db, redis

	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Fatal(http.ListenAndServe(addr, nil))
}
