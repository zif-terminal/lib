package solana_dex

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// Global rate limiter shared across all solana_dex.Client instances.
//
// The documented Helius free tier is "10 req/s" but in practice the gateway
// surfaces 429 well below that, even for sequential single-shot requests with
// no concurrency. Empirically we observed 60s+ retry storms at 8 RPS during
// the e3731188 backfill (May 2026): every single page request consumed the
// entire 5-retry budget and still failed. Dropping the steady-state ceiling
// to 1 RPS (burst 1) is what actually keeps us under the gateway's hidden
// threshold. Combined with the Retry-After / jittered backoff added in
// liveGet, this is enough to make multi-thousand-tx backfills make progress
// on the free tier instead of stalling.
//
// If we ever upgrade Helius tiers, bump these constants — they are the only
// throttle. Burst is intentionally 1 (not 2) because a burst of 2 produced
// observable 429s on the very first page request after an idle window: the
// gateway's bucket appears to refill more slowly than 1/s, so consuming
// 2 tokens back-to-back trips it.
//
// Developer-tier bump (May 2026, helius credit audit task #55): the account
// is now on the paid Developer plan, documented at 10 RPS on Enhanced APIs.
// We run at 8 RPS (steady) with a small burst=2 to leave headroom for the
// gateway's hidden tightening that historically punished us at the
// documented ceiling.
var globalLimiter = newRateLimiter(8, 2)

// jitterRand is package-local rand source for backoff jitter. We don't need
// crypto-grade randomness; the goal is purely to desynchronise multiple
// concurrent retries.
var (
	jitterRandMu sync.Mutex
	jitterRand   = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// backoffWithJitter returns the next backoff duration for the given attempt
// (0-indexed). The schedule is base * 2^attempt + random(0..jitter), capped
// at maxBackoff. Jitter prevents synchronised retries from multiple
// goroutines or back-to-back failures.
//
// Schedule for base=2s, jitter=1s, max=300s:
//   attempt 0: 2s  + 0..1s
//   attempt 1: 4s  + 0..1s
//   attempt 2: 8s  + 0..1s
//   attempt 3: 16s + 0..1s
//   attempt 4: 32s + 0..1s
//   attempt 5: 64s + 0..1s
//   attempt 6: 128s + 0..1s
//   attempt 7: 256s + 0..1s
//   attempt 8+: 300s (capped)
func backoffWithJitter(attempt int, base, jitter, maxBackoff time.Duration) time.Duration {
	// Compute base * 2^attempt without overflow.
	d := base
	for i := 0; i < attempt; i++ {
		if d >= maxBackoff {
			d = maxBackoff
			break
		}
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	// Add jitter.
	if jitter > 0 {
		jitterRandMu.Lock()
		j := time.Duration(jitterRand.Int63n(int64(jitter)))
		jitterRandMu.Unlock()
		d += j
		if d > maxBackoff {
			d = maxBackoff
		}
	}
	return d
}

// rateLimiter is a token-bucket limiter (mirrors the lighter package pattern).
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

// Wait blocks until a token is available or ctx is cancelled.
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

		wait := time.Duration((1 - r.tokens) / r.rate * float64(time.Second))
		r.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
