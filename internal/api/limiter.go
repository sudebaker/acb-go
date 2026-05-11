package api

import (
	"sync"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*rate.Limiter
	rate    rate.Limit
	burst   int
}

func NewRateLimiter(r rate.Limit, burst int) *RateLimiter {
	return &RateLimiter{
		clients: make(map[string]*rate.Limiter),
		rate:    r,
		burst:   burst,
	}
}

func (rl *RateLimiter) Allow(name string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, ok := rl.clients[name]
	if !ok {
		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.clients[name] = limiter
	}

	return limiter.Allow()
}
