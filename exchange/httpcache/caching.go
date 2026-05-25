package httpcache

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// caching is the filesystem-backed cache transport. Stateless beyond cfg —
// every Get/Post resolves a cache path from the key and read/writes a single
// JSON file. Concurrent calls for the same key may both perform a live fetch
// and both write the same file; the last writer wins. Safe because writes
// are atomic (rename) and the body is content-addressed by URL.
type caching struct {
	cfg Config
}

// entry is the on-disk JSON shape. body_bytes holds the literal response
// body as a string (responses are JSON in every exchange we currently
// support; if a future exchange returns binary, swap this to base64).
//
// Storage format: on disk the JSON envelope itself is gzipped end-to-end.
// Detection on read uses the gzip magic bytes (0x1f 0x8b) at file start;
// pre-gzip-rollout entries lack the magic and are read as plain JSON. The
// Compressed field inside the envelope is set true on new writes (purely
// informational — the magic bytes are the actual detection signal).
type entry struct {
	Exchange   string    `json:"exchange"`
	URL        string    `json:"url"`
	Method     string    `json:"method"`
	FetchedAt  time.Time `json:"fetched_at"`
	BodyBytes  string    `json:"body_bytes"`
	Compressed bool      `json:"compressed,omitempty"`
}

// gzipMagic is the 2-byte signature at the start of every gzip stream
// (RFC 1952). Used to distinguish gzipped cache files from legacy
// plain-JSON files written before compression was added.
var gzipMagic = []byte{0x1f, 0x8b}

func (c *caching) IsEnabled() bool { return true }
func (c *caching) Mode() Mode      { return c.cfg.Mode }

func (c *caching) Get(ctx context.Context, exchange, url, authPrincipal string, live Fetcher) ([]byte, error) {
	return c.do(ctx, "GET", exchange, url, authPrincipal, nil, live)
}

func (c *caching) Post(ctx context.Context, exchange, url, authPrincipal string, body []byte, live Fetcher) ([]byte, error) {
	// POSTs are never cached (per spec). Still log a MISS-by-policy so the
	// operator can see why nothing is being persisted from Hyperliquid.
	if c.cfg.Mode == ModeReadOnly {
		// In read_only mode an uncacheable POST would otherwise silently
		// hit the live API; surface that the call is bypassing the cache.
		log.Printf("[cache PASSTHROUGH] exchange=%s method=POST url=%s mode=%s (POST never cached)", exchange, url, c.cfg.Mode)
	}
	return live(ctx)
}

func (c *caching) do(ctx context.Context, method, exchange, url, authPrincipal string, body []byte, live Fetcher) ([]byte, error) {
	if authPrincipal == "" {
		// Same URL with different auth returns different data. Refusing
		// here is the only way to avoid cross-account aliasing, which
		// would silently corrupt cached responses.
		return nil, fmt.Errorf("httpcache: empty auth principal | exchange=%s url=%s", exchange, url)
	}
	safeURL := redactSecrets(url)

	// Allowlist: if configured, principals not on the list bypass the
	// cache entirely (no read, no write). In read_only mode we still
	// must error rather than silently hit the live API, otherwise
	// "force-replay against frozen inputs" loses its guarantee.
	if len(c.cfg.PrincipalAllowlist) > 0 && !c.cfg.PrincipalAllowlist[authPrincipal] {
		if c.cfg.Mode == ModeReadOnly {
			return nil, &ReadOnlyMissError{Exchange: exchange, URL: safeURL}
		}
		log.Printf("[cache SKIP-PRINCIPAL] exchange=%s url=%s principal=%s (not in allowlist)", exchange, safeURL, authPrincipal)
		return live(ctx)
	}

	path := c.entryPath(exchange, method, safeURL, authPrincipal, body)

	if c.cfg.Mode != ModeRefresh {
		if cached, ok, err := c.read(path); err != nil {
			return nil, fmt.Errorf("httpcache: read %s: %w", path, err)
		} else if ok {
			if !c.entryFresh(cached, exchange, safeURL) {
				log.Printf("[cache MISS] exchange=%s url=%s mode=%s (stale, age=%v)", exchange, safeURL, c.cfg.Mode, c.cfg.Clock().Sub(cached.FetchedAt))
			} else {
				log.Printf("[cache HIT] exchange=%s url=%s age=%v", exchange, safeURL, c.cfg.Clock().Sub(cached.FetchedAt))
				return []byte(cached.BodyBytes), nil
			}
		} else {
			log.Printf("[cache MISS] exchange=%s url=%s mode=%s", exchange, safeURL, c.cfg.Mode)
		}
	}

	if c.cfg.Mode == ModeReadOnly {
		return nil, &ReadOnlyMissError{Exchange: exchange, URL: safeURL}
	}

	respBody, err := live(ctx)
	if err != nil {
		return nil, err
	}
	if err := c.write(path, &entry{
		Exchange:  exchange,
		URL:       safeURL,
		Method:    method,
		FetchedAt: c.cfg.Clock(),
		BodyBytes: string(respBody),
	}); err != nil {
		log.Printf("[cache WRITE-FAIL] exchange=%s url=%s err=%v", exchange, safeURL, err)
	}
	return respBody, nil
}

// redactSecrets strips known sensitive query parameters (api-key, api_key,
// apiKey, token, auth) from a URL so they are not persisted to the cache
// entry or echoed in log lines. The redacted form is also used in the
// cache-key hash: the per-account auth principal scopes the entry instead,
// and stripping the secret means a key-rotation does not orphan all cached
// entries for the same wallet.
func redactSecrets(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	changed := false
	for _, k := range []string{"api-key", "api_key", "apiKey", "token", "auth"} {
		if q.Has(k) {
			q.Set(k, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// entryFresh returns true if the cached entry is still usable. Historical
// endpoints (matched by exchange's CacheableForeverPatterns) never expire;
// everything else expires after LiveTTL.
func (c *caching) entryFresh(e *entry, exchange, url string) bool {
	if isHistorical(c.cfg.CacheableForeverPatterns, exchange, url) {
		return true
	}
	age := c.cfg.Clock().Sub(e.FetchedAt)
	return age < c.cfg.LiveTTL
}

// isHistorical reports whether the given (exchange, url) matches any of the
// configured cache-forever substrings.
func isHistorical(patterns map[string][]string, exchange, url string) bool {
	for _, pat := range patterns[exchange] {
		if pat == "" {
			continue
		}
		if strings.Contains(url, pat) {
			return true
		}
	}
	return false
}

func (c *caching) entryPath(exchange, method, url, authPrincipal string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(exchange))
	h.Write([]byte{0})
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(url))
	h.Write([]byte{0})
	h.Write([]byte(authPrincipal))
	if len(body) > 0 {
		h.Write([]byte{0})
		h.Write(body)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	// Shard into two 2-char subdirs so no single directory holds more
	// than ~65k entries even at scale.
	return filepath.Join(c.cfg.Dir, sum[:2], sum[2:4], sum+".json")
}

func (c *caching) read(path string) (*entry, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	// Backward compatible: pre-gzip entries are plain JSON. Detect gzip
	// via the 2-byte magic at the start (RFC 1952).
	data := raw
	if len(raw) >= 2 && bytes.Equal(raw[:2], gzipMagic) {
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, false, fmt.Errorf("gzip open %s: %w", path, err)
		}
		decompressed, err := io.ReadAll(gr)
		_ = gr.Close()
		if err != nil {
			return nil, false, fmt.Errorf("gzip read %s: %w", path, err)
		}
		data = decompressed
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false, fmt.Errorf("decode %s: %w", path, err)
	}
	return &e, true, nil
}

func (c *caching) write(path string, e *entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	e.Compressed = true
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	// Gzip the whole envelope. Default compression level — on JSON
	// payloads the level-1 vs level-9 size delta is small and we trade
	// CPU for disk. Sub-ms per entry on typical exchange responses.
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		_ = gw.Close()
		return fmt.Errorf("gzip write: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("gzip close: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
