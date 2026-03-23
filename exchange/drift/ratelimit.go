package drift

import (
	"context"
	"sync"
	"time"
)

// Global rate limiter shared across all drift.Client instances.
// Drift allows ~600 req/min (10 req/s). We target 8 req/s to leave headroom.
var globalLimiter = newRateLimiter(8, 8)

// rateLimiter implements a token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newRateLimiter(ratePerSecond, burst float64) *rateLimiter {
	return &rateLimiter{
		tokens:   burst,
		maxBurst: burst,
		rate:     ratePerSecond,
		lastTime: time.Now(),
	}
}

// Wait blocks until a token is available or the context is cancelled.
func (r *rateLimiter) Wait(ctx context.Context) error {
	for {
		r.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(r.lastTime).Seconds()
		r.tokens += elapsed * r.rate
		if r.tokens > r.maxBurst {
			r.tokens = r.maxBurst
		}
		r.lastTime = now

		if r.tokens >= 1 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}

		// Calculate how long until a token is available
		wait := time.Duration((1 - r.tokens) / r.rate * float64(time.Second))
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
