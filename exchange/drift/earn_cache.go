package drift

import (
	"sync"
	"time"
)

// earnCacheTTL bounds how long a memoized earn payload is served.
//
// It must be:
//   - short enough that a real earn change can't be served stale for long, and
//   - long enough to cover a single sync cycle's back-to-back per-sub-account
//     FetchHistoricalBalanceSnapshots calls (which fire within seconds of each
//     other for every sub-account of a wallet).
//
// The earn endpoint (/authority/{wallet}/snapshots/earn) feeds
// FetchHistoricalBalanceSnapshots — historical backfill snapshots, which are
// immutable past points — NOT the live current balance (that comes from
// /user/{accountID} in FetchBalances, which is intentionally left uncached).
// So this cache can never serve a stale *current* balance; the short TTL is
// defence-in-depth so that even the most-recent historical snapshot is at most
// one TTL behind.
const earnCacheTTL = 60 * time.Second

// earnCacheEntry is a single memoized earn payload with its fetch time.
type earnCacheEntry struct {
	resp      *driftEarnResponse
	fetchedAt time.Time
}

// earnResponseCache is a TTL-bounded memo of earn payloads keyed by request
// URL. Safe for concurrent use.
//
// It exists to collapse the N identical earn fetches a wallet with N
// sub-accounts would otherwise make per sync cycle: the earn endpoint returns
// snapshots for ALL sub-accounts under a wallet, so every sub-account's
// FetchHistoricalBalanceSnapshots call re-downloaded the same full payload and
// then filtered it to its own accountID.
type earnResponseCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]earnCacheEntry
}

// newEarnResponseCache creates an earn cache with the given TTL.
func newEarnResponseCache(ttl time.Duration) *earnResponseCache {
	return &earnResponseCache{
		ttl:     ttl,
		entries: make(map[string]earnCacheEntry),
	}
}

// get returns the cached response for key when present and still fresh.
// A stale entry is evicted and reported as a miss.
func (ec *earnResponseCache) get(key string) (*driftEarnResponse, bool) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	entry, ok := ec.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.fetchedAt) > ec.ttl {
		delete(ec.entries, key)
		return nil, false
	}
	return entry.resp, true
}

// set stores resp for key stamped at the current time.
func (ec *earnResponseCache) set(key string, resp *driftEarnResponse) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.entries[key] = earnCacheEntry{resp: resp, fetchedAt: time.Now()}
}

// globalEarnCache memoizes earn payloads across drift Client instances.
//
// This MUST be package-level: the snapshot syncer constructs a FRESH drift
// Client per account (exchange.GetClientWithDB("drift") → drift.NewClient()),
// so an instance-scoped cache would never be shared across a wallet's
// sub-accounts and the dedup would never fire. Keying by full request URL
// (which embeds baseURL + wallet authority) keeps prod entries consistent and
// prevents test servers on distinct ports from ever colliding.
var globalEarnCache = newEarnResponseCache(earnCacheTTL)
