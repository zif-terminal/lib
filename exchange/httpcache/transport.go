package httpcache

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Mode controls the caching transport's behavior. See package docs for full
// semantics.
type Mode string

const (
	ModeDisabled    Mode = "disabled"
	ModeReadThrough Mode = "read_through"
	ModeReadOnly    Mode = "read_only"
	ModeRefresh     Mode = "refresh"
)

// Fetcher performs the live HTTP request when the cache doesn't satisfy the
// call. It is supplied by the per-exchange client and must NOT consult the
// cache itself — that is the transport's job.
type Fetcher func(ctx context.Context) ([]byte, error)

// Transport is the HTTP-layer cache boundary every exchange client routes
// reads through. Implementations: Passthrough (no caching) and Caching.
type Transport interface {
	// Get returns the response body for a GET. exchange is the canonical
	// exchange name (e.g. "lighter"). url is the full URL including query
	// params. authPrincipal is the per-account identifier that scopes the
	// auth context (api_key, account_index, wallet address). live is the
	// live-fetch callback used on cache miss; the transport invokes it at
	// most once per call.
	Get(ctx context.Context, exchange, url, authPrincipal string, live Fetcher) ([]byte, error)

	// Post mirrors Get for POST endpoints. The caching transport never
	// caches POST responses — the request is always delegated to live —
	// but routing through Post keeps the per-exchange code uniform and
	// lets us add future cacheable-POST allowlists without changing
	// callers.
	Post(ctx context.Context, exchange, url, authPrincipal string, body []byte, live Fetcher) ([]byte, error)

	// IsEnabled reports whether the transport is actively caching. Used
	// by tests + per-exchange code that wants to skip cache-related work
	// (e.g. computing an auth principal) when caching is disabled.
	IsEnabled() bool

	// Mode returns the configured Mode. Useful for log lines.
	Mode() Mode
}

// Config bundles the knobs governing a caching Transport. All fields have
// sensible defaults applied by NewFromEnv.
type Config struct {
	Mode Mode

	// Dir is the filesystem path where cache entries live.
	Dir string

	// LiveTTL is how long "live" (non-historical) responses are kept.
	// Historical endpoints in CacheableForeverPatterns are kept forever.
	LiveTTL time.Duration

	// CacheableForeverPatterns is a per-exchange map: exchange name →
	// list of URL-path substrings that mean "historical, cache forever".
	// E.g. {"lighter": ["/trades", "/positionFunding", ...]}.
	CacheableForeverPatterns map[string][]string

	// PrincipalAllowlist, if non-empty, restricts caching to the listed
	// auth principals. Calls for any other principal fall through to live
	// fetch with no cache read or write (HIT/MISS logs still emit so the
	// skip is visible). Empty (default) = cache all principals.
	PrincipalAllowlist map[string]bool

	// Clock is used by tests to control time. Defaults to time.Now.
	Clock func() time.Time
}

// DefaultCacheableForeverPatterns is the per-exchange historical-endpoint
// allowlist. Be explicit; do not try to be clever — guess-based caching of
// live endpoints is a footgun.
//
// Each entry is a substring tested against the request URL's path+query. Any
// match → cache forever. No match → LiveTTL.
var DefaultCacheableForeverPatterns = map[string][]string{
	"lighter": {
		// Lighter's history endpoints (/trades, /liquidations,
		// /positionFunding, /{deposit,withdraw,transfer}/history) paginate
		// newest-first with "&cursor=<c>". Only the cursor-anchored pages
		// are immutable slices safe to cache forever.
		//
		// CRITICAL (#266 — same footgun the solana_dex block documents): the
		// FIRST page of each walk has NO "&cursor=" — it is the newest-data
		// tail ("/positionFunding?account_index=…&limit=100"). Matching the
		// bare endpoint substring pinned that first page forever, so
		// funding/trades booked after the entry was written were never
		// re-fetched (froze the Lighter Analytics feed). Gate on "&cursor="
		// so first/tail pages fall to LiveTTL (re-fetched each sync) while
		// cursor pages stay cached forever.
		"&cursor=",
	},
	"hyperliquid": {
		`"type":"userFills"`,
		`"type":"userFillsByTime"`,
		`"type":"userNonFundingLedgerUpdates"`,
		`"type":"delegatorHistory"`,
	},
	"drift": {
		"/tradeRecords/",
		"/swapRecords/",
		"/fundingPaymentRecords/",
		"/depositRecords/",
		"/settlePnlRecords/",
	},
	"solana_dex": {
		// Cursor-anchored transaction pages (URL contains "&before=<sig>")
		// are immutable: Helius's enhanced-tx endpoint orders newest-first
		// and `before=` pins the response to a fixed slice ending at that
		// signature. We can cache those forever.
		//
		// CRITICAL: the FIRST page of a paginated walk has NO `&before=`
		// cursor (URL ends in `/transactions?api-key=...&limit=100` only).
		// That page represents "newest data right now" and MUST NOT be
		// cached forever, otherwise new txs discovered later would never
		// be picked up. Earlier versions of this allowlist matched on
		// `/transactions` alone and silently pinned the first page,
		// breaking incremental sync. The current substring matches only
		// when `&before=` is present in the URL, so the first page falls
		// through to the LiveTTL bucket (refetched on each sync cycle)
		// while all subsequent cursor pages are kept forever.
		"&before=",
	},
}

// DefaultCacheDir is where cache files live when EXCHANGE_API_CACHE_DIR is
// unset. Inside the account_sync container this is the conventional location
// for service-local cache data; the docker-compose mount maps it to the host
// at /home/ubuntu/zif-cache/exchange so cache state survives container
// rebuilds.
const DefaultCacheDir = "/var/cache/zif/exchange"

// NewFromEnv constructs a Transport from EXCHANGE_API_CACHE_MODE +
// EXCHANGE_API_CACHE_DIR + EXCHANGE_API_CACHE_LIVE_TTL +
// EXCHANGE_API_CACHE_PRINCIPAL_ALLOWLIST. If EXCHANGE_API_CACHE_MODE is
// unset, mode defaults to read_through. To fully disable the cache set
// EXCHANGE_API_CACHE_MODE=disabled. Any unknown mode returns an error so
// misconfiguration is surfaced immediately.
func NewFromEnv() (Transport, error) {
	raw := strings.TrimSpace(os.Getenv("EXCHANGE_API_CACHE_MODE"))
	if raw == "" {
		raw = string(ModeReadThrough)
	}
	mode := Mode(raw)
	switch mode {
	case ModeDisabled:
		return Passthrough(), nil
	case ModeReadThrough, ModeReadOnly, ModeRefresh:
		// fall through
	default:
		return nil, fmt.Errorf("httpcache: invalid EXCHANGE_API_CACHE_MODE %q (want disabled|read_through|read_only|refresh)", raw)
	}

	dir := strings.TrimSpace(os.Getenv("EXCHANGE_API_CACHE_DIR"))
	if dir == "" {
		dir = DefaultCacheDir
	}

	ttl := time.Hour
	if raw := strings.TrimSpace(os.Getenv("EXCHANGE_API_CACHE_LIVE_TTL")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("httpcache: invalid EXCHANGE_API_CACHE_LIVE_TTL %q: %w", raw, err)
		}
		ttl = parsed
	}

	allowlist := parsePrincipalAllowlist(os.Getenv("EXCHANGE_API_CACHE_PRINCIPAL_ALLOWLIST"))

	return New(Config{
		Mode:                     mode,
		Dir:                      dir,
		LiveTTL:                  ttl,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		PrincipalAllowlist:       allowlist,
	})
}

// parsePrincipalAllowlist parses a comma-separated principal list. Empty
// entries (from leading/trailing/duplicate commas) are dropped. An empty
// result indicates "no allowlist — cache everything".
func parsePrincipalAllowlist(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := make(map[string]bool)
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// New constructs a caching Transport with explicit config. Used by tests; in
// production callers should prefer NewFromEnv.
func New(cfg Config) (Transport, error) {
	if cfg.Mode == "" || cfg.Mode == ModeDisabled {
		return Passthrough(), nil
	}
	if cfg.Dir == "" {
		return nil, fmt.Errorf("httpcache: Config.Dir is required for mode %s", cfg.Mode)
	}
	if cfg.LiveTTL <= 0 {
		cfg.LiveTTL = time.Hour
	}
	if cfg.CacheableForeverPatterns == nil {
		cfg.CacheableForeverPatterns = DefaultCacheableForeverPatterns
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("httpcache: mkdir %q: %w", cfg.Dir, err)
	}
	return &caching{cfg: cfg}, nil
}

// ReadOnlyMissError is returned when the configured mode is read_only and the
// requested URL is not in the cache.
type ReadOnlyMissError struct {
	Exchange string
	URL      string
}

func (e *ReadOnlyMissError) Error() string {
	return fmt.Sprintf("httpcache: cache miss in read_only mode | exchange=%s url=%s", e.Exchange, e.URL)
}

// globalPassthrough is the singleton passthrough transport. It is stateless
// and safe for shared use across all exchange clients.
var (
	globalPassthroughOnce sync.Once
	globalPassthroughT    *passthrough
)

// Passthrough returns the no-op transport: it always delegates to the live
// Fetcher and never reads or writes the cache. Used when caching is disabled.
func Passthrough() Transport {
	globalPassthroughOnce.Do(func() {
		globalPassthroughT = &passthrough{}
	})
	return globalPassthroughT
}
