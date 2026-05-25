// Package httpcache provides a caching layer for raw exchange API responses,
// sitting between an exchange client's HTTP layer and the live API.
//
// # Why
//
// Force-replays of an account (e.g. when iterating on accounting fixes) require
// re-fetching every page of /trades, /transfer/history, /withdraw/history, etc.
// across multiple exchanges. This hits exchange rate limits and makes iteration
// slow. The httpcache transport intercepts HTTP responses at the per-exchange
// doGet/doPost boundary so replays can hit local storage instead of the live API.
//
// # Wiring
//
// Each exchange client (lighter, hyperliquid, drift, solana_dex) holds an
// optional Transport. When Transport.IsEnabled() is false (the case when
// EXCHANGE_API_CACHE_MODE=disabled), the client uses its existing http.Client
// directly and behaviour is unchanged. Otherwise GET responses flow through
// Get(...), which consults the cache and (depending on mode) either returns a
// cached body, fetches fresh from the supplied Fetcher and writes it to cache,
// or returns an error.
//
// POST responses (used by Hyperliquid for read queries, and by every exchange
// for trade submission etc.) flow through Post(...). The caching transport
// honours mode=disabled/passthrough for POSTs, otherwise it short-circuits POST
// to the Fetcher with no caching — POST is never cached.
//
// # Modes
//
// Set EXCHANGE_API_CACHE_MODE to one of:
//
//   - read_through (default, when env var unset) — try cache first; on miss,
//     fetch live and write to cache.
//   - disabled — passthrough; no caching. Use to fully bypass: set
//     EXCHANGE_API_CACHE_MODE=disabled.
//   - read_only — try cache first; on miss, return an error containing the URL.
//     Use this to force replays against ONLY cached data (e.g. to confirm an
//     accounting fix produces the same result against frozen inputs).
//   - refresh — always fetch live and write to cache. Use to (re)warm the cache.
//
// # Cache key
//
// SHA-256 of "exchange\x00method\x00url\x00auth_principal" plus, for POST,
// "\x00body". auth_principal is the per-account API key or account
// identifier — never blank, otherwise the same URL with different auth would
// alias. Auth headers themselves are NEVER stored in the cache entry.
//
// # Storage
//
// Filesystem (one gzipped JSON file per response) at $EXCHANGE_API_CACHE_DIR,
// falling back to /var/cache/zif/exchange (the DefaultCacheDir constant). The
// docker-compose stack mounts /home/ubuntu/zif-cache/exchange on the host to
// that path so cache state survives container rebuilds.
//
// Files are sharded to 2-level subdirs by the first 4 hex chars of the key
// (e.g. ab/cd/abcd123...json) to avoid single-directory bloat. Compression is
// transparent — files written before the gzip rollout (plain JSON) are still
// readable; the reader detects gzip via the 0x1f 0x8b magic bytes.
//
// To clear:
//
//	rm -rf "$EXCHANGE_API_CACHE_DIR"   # or /var/cache/zif/exchange if unset
//
// # Scoping
//
// EXCHANGE_API_CACHE_PRINCIPAL_ALLOWLIST takes a comma-separated list of auth
// principals (api keys / account ids — whatever the per-exchange client
// passes as authPrincipal). When set, only listed principals are cached;
// others fall through to live with no read or write. Unset (default) caches
// every principal. Use during heavy iteration on a handful of accounts to
// avoid filling disk for accounts you're not touching.
//
// # TTL
//
// Each exchange registers a set of "cacheable forever" URL-path substrings
// (e.g. /trades, /transfer/history, /deposit/history, /withdraw/history,
// /positionFunding, /liquidations, userFills, userNonFundingLedgerUpdates,
// fundingPayments, deposits, settlePnl). Matches are cached without expiry —
// these endpoints return immutable historical records.
//
// Anything not matching is treated as "live" data (account state, mark prices,
// fee info, address balances) and gets a 1-hour TTL (override with
// EXCHANGE_API_CACHE_LIVE_TTL, e.g. "10m" or "24h").
//
// # Known limitations
//
//   - Hyperliquid's read API is POST-only, so its responses are NEVER cached.
//     The transport still runs through the same code path for uniformity, but
//     it always passes through to the live API. If a Hyperliquid replay needs
//     to avoid hitting the live API, run a separate proxy.
//   - The cache key for paginated endpoints includes the cursor in the URL, so
//     each page is its own entry. Mid-page partial responses are not cached
//     (the live HTTP layer would have returned an error and we never get to
//     the cache write).
//   - We do not negative-cache 4xx responses. Errors are propagated to the
//     caller and the cache is left untouched.
//   - Cache entries do not record response headers. If a downstream consumer
//     starts to care about a header (e.g. ratelimit metadata), this needs to
//     grow.
package httpcache
