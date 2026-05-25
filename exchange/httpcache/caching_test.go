package httpcache

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustGunzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return out
}

func newTestTransport(t *testing.T, mode Mode, ttl time.Duration, clock func() time.Time) Transport {
	t.Helper()
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     mode,
		Dir:                      dir,
		LiveTTL:                  ttl,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		Clock:                    clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tr
}

func TestCachingTransport_StoresAndRetrieves(t *testing.T) {
	tr := newTestTransport(t, ModeReadThrough, time.Hour, time.Now)
	ctx := context.Background()

	calls := 0
	live := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`{"x":1}`), nil
	}

	url := "https://example.com/api/v1/trades?account_index=1&cursor=abc"
	body, err := tr.Get(ctx, "lighter", url, "principal-1", live)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if string(body) != `{"x":1}` {
		t.Fatalf("first Get body = %q, want %q", body, `{"x":1}`)
	}
	if calls != 1 {
		t.Fatalf("first Get: live calls = %d, want 1", calls)
	}

	body, err = tr.Get(ctx, "lighter", url, "principal-1", live)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if string(body) != `{"x":1}` {
		t.Fatalf("second Get body = %q, want %q", body, `{"x":1}`)
	}
	if calls != 1 {
		t.Fatalf("second Get: live calls = %d, want 1 (should be a hit)", calls)
	}
}

func TestCachingTransport_ReadOnlyMissReturnsError(t *testing.T) {
	tr := newTestTransport(t, ModeReadOnly, time.Hour, time.Now)
	ctx := context.Background()

	url := "https://example.com/api/v1/trades?account_index=1"
	called := false
	live := func(context.Context) ([]byte, error) {
		called = true
		return []byte(`{"should":"not be called"}`), nil
	}

	_, err := tr.Get(ctx, "lighter", url, "principal-1", live)
	if err == nil {
		t.Fatalf("expected error on read_only miss, got nil")
	}
	var miss *ReadOnlyMissError
	if !errors.As(err, &miss) {
		t.Fatalf("expected ReadOnlyMissError, got %T: %v", err, err)
	}
	if miss.URL != url {
		t.Fatalf("ReadOnlyMissError.URL = %q, want %q", miss.URL, url)
	}
	if !strings.Contains(err.Error(), url) {
		t.Fatalf("error message %q does not contain URL %q", err.Error(), url)
	}
	if called {
		t.Fatalf("live fetcher was called in read_only mode")
	}
}

func TestCachingTransport_TTLOnLiveEndpoints(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	tr := newTestTransport(t, ModeReadThrough, time.Hour, func() time.Time { return clock() })
	ctx := context.Background()

	calls := 0
	live := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`{"call":` + string(rune('0'+calls)) + `}`), nil
	}

	// A "live" endpoint per the default allowlist (not historical).
	url := "https://mainnet.zklighter.elliot.ai/api/v1/account?by=l1_address&value=0x123"

	if _, err := tr.Get(ctx, "lighter", url, "principal-1", live); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("first Get: calls = %d, want 1", calls)
	}

	// Within TTL → hit.
	now = now.Add(30 * time.Minute)
	if _, err := tr.Get(ctx, "lighter", url, "principal-1", live); err != nil {
		t.Fatalf("warm Get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("within-TTL Get: calls = %d, want 1 (hit)", calls)
	}

	// Past TTL → miss + refetch.
	now = now.Add(45 * time.Minute)
	if _, err := tr.Get(ctx, "lighter", url, "principal-1", live); err != nil {
		t.Fatalf("stale Get: %v", err)
	}
	if calls != 2 {
		t.Fatalf("past-TTL Get: calls = %d, want 2 (refetch)", calls)
	}
}

func TestCachingTransport_HistoricalNeverExpires(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	tr := newTestTransport(t, ModeReadThrough, time.Hour, func() time.Time { return clock() })
	ctx := context.Background()

	calls := 0
	live := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`{"trades":[]}`), nil
	}

	url := "https://mainnet.zklighter.elliot.ai/api/v1/trades?account_index=1&cursor=abc"

	if _, err := tr.Get(ctx, "lighter", url, "principal-1", live); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	now = now.Add(365 * 24 * time.Hour)
	if _, err := tr.Get(ctx, "lighter", url, "principal-1", live); err != nil {
		t.Fatalf("year-later Get: %v", err)
	}
	if calls != 1 {
		t.Fatalf("historical URL was re-fetched after TTL: calls = %d, want 1", calls)
	}
}

func TestCachingTransport_NeverCachesPOST(t *testing.T) {
	tr := newTestTransport(t, ModeReadThrough, time.Hour, time.Now)
	ctx := context.Background()

	calls := 0
	live := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`{"call":` + string(rune('0'+calls)) + `}`), nil
	}

	url := "https://api.hyperliquid.xyz/info"
	body := []byte(`{"type":"userFills","user":"0xabc"}`)

	if _, err := tr.Post(ctx, "hyperliquid", url, "0xabc", body, live); err != nil {
		t.Fatalf("first Post: %v", err)
	}
	if _, err := tr.Post(ctx, "hyperliquid", url, "0xabc", body, live); err != nil {
		t.Fatalf("second Post: %v", err)
	}
	if calls != 2 {
		t.Fatalf("POST was cached: calls = %d, want 2", calls)
	}
}

func TestCachingTransport_RefreshAlwaysFetches(t *testing.T) {
	tr := newTestTransport(t, ModeRefresh, time.Hour, time.Now)
	ctx := context.Background()

	calls := 0
	live := func(context.Context) ([]byte, error) {
		calls++
		return []byte(`fresh`), nil
	}

	url := "https://example.com/api/v1/trades"
	if _, err := tr.Get(ctx, "lighter", url, "p", live); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := tr.Get(ctx, "lighter", url, "p", live); err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls != 2 {
		t.Fatalf("refresh mode hit cache: calls = %d, want 2", calls)
	}
}

func TestCachingTransport_AuthPrincipalScopesKey(t *testing.T) {
	tr := newTestTransport(t, ModeReadThrough, time.Hour, time.Now)
	ctx := context.Background()

	url := "https://example.com/api/v1/trades?account_index=1"

	body1 := []byte(`{"who":"alice"}`)
	body2 := []byte(`{"who":"bob"}`)

	if got, err := tr.Get(ctx, "lighter", url, "alice", func(context.Context) ([]byte, error) { return body1, nil }); err != nil {
		t.Fatalf("alice Get: %v", err)
	} else if string(got) != string(body1) {
		t.Fatalf("alice body = %q, want %q", got, body1)
	}
	if got, err := tr.Get(ctx, "lighter", url, "bob", func(context.Context) ([]byte, error) { return body2, nil }); err != nil {
		t.Fatalf("bob Get: %v", err)
	} else if string(got) != string(body2) {
		t.Fatalf("bob body = %q, want %q (different principal must miss)", got, body2)
	}
}

func TestCachingTransport_EmptyAuthPrincipalErrors(t *testing.T) {
	tr := newTestTransport(t, ModeReadThrough, time.Hour, time.Now)
	ctx := context.Background()

	_, err := tr.Get(ctx, "lighter", "https://example.com/trades", "", func(context.Context) ([]byte, error) { return []byte("x"), nil })
	if err == nil {
		t.Fatalf("expected error on empty auth principal, got nil")
	}
	if !strings.Contains(err.Error(), "empty auth principal") {
		t.Fatalf("error = %v, want 'empty auth principal'", err)
	}
}

func TestCachingTransport_PersistsToDisk(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     ModeReadThrough,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	body := []byte(`{"persisted":true}`)
	if _, err := tr.Get(ctx, "lighter", "https://example.com/trades?cursor=a", "p", func(context.Context) ([]byte, error) {
		return body, nil
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Walk dir, count .json files.
	count := 0
	err = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".json") {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if count != 1 {
		t.Fatalf("dir entries = %d, want 1", count)
	}
}

func TestCachingTransport_RedactsAPIKeyInStoredURL(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     ModeReadThrough,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	url := "https://api.helius.xyz/v0/addresses/abc/transactions?api-key=SECRET-KEY-xyz&limit=100"
	if _, err := tr.Get(ctx, "solana_dex", url, "principal-1", func(context.Context) ([]byte, error) {
		return []byte(`{}`), nil
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	found := false
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		// Cache files are gzipped end-to-end. The raw file must NEVER
		// contain the api-key in plaintext (gzip is not encryption, but
		// even pre-decompression the bytes are scrambled), and the
		// decompressed envelope must carry "REDACTED" in the stored URL.
		// Check both layers explicitly.
		if bytes.Contains(raw, []byte("SECRET-KEY-xyz")) {
			t.Fatalf("cache entry %s contains the raw api-key in gzipped bytes", p)
		}
		decompressed := mustGunzip(t, raw)
		s := string(decompressed)
		if strings.Contains(s, "SECRET-KEY-xyz") {
			t.Fatalf("cache entry %s decompressed contains the raw api-key: %s", p, s)
		}
		if strings.Contains(s, "REDACTED") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("expected cache entry to contain REDACTED placeholder")
	}
}

func TestNewFromEnv_DefaultsToReadThrough(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EXCHANGE_API_CACHE_MODE", "")
	t.Setenv("EXCHANGE_API_CACHE_DIR", dir)
	t.Setenv("EXCHANGE_API_CACHE_PRINCIPAL_ALLOWLIST", "")
	tr, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	if !tr.IsEnabled() {
		t.Fatalf("expected enabled transport with default mode")
	}
	if tr.Mode() != ModeReadThrough {
		t.Fatalf("default mode = %s, want read_through", tr.Mode())
	}
}

func TestNewFromEnv_ExplicitDisabled(t *testing.T) {
	t.Setenv("EXCHANGE_API_CACHE_MODE", "disabled")
	tr, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	if tr.IsEnabled() {
		t.Fatalf("expected disabled transport for explicit 'disabled'")
	}
}

func TestNewFromEnv_Invalid(t *testing.T) {
	t.Setenv("EXCHANGE_API_CACHE_MODE", "garbage")
	_, err := NewFromEnv()
	if err == nil {
		t.Fatalf("expected error for invalid mode")
	}
}

func TestNewFromEnv_ReadThrough(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("EXCHANGE_API_CACHE_MODE", "read_through")
	t.Setenv("EXCHANGE_API_CACHE_DIR", dir)
	tr, err := NewFromEnv()
	if err != nil {
		t.Fatalf("NewFromEnv: %v", err)
	}
	if !tr.IsEnabled() {
		t.Fatalf("expected enabled transport")
	}
	if tr.Mode() != ModeReadThrough {
		t.Fatalf("Mode = %s, want read_through", tr.Mode())
	}
}

func TestCachingTransport_WritesGzipped(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     ModeReadThrough,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body := []byte(`{"gzipped":true,"filler":"` + strings.Repeat("x", 4096) + `"}`)
	if _, err := tr.Get(context.Background(), "lighter", "https://example.com/trades?cursor=z", "p", func(context.Context) ([]byte, error) {
		return body, nil
	}); err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Find the written entry and inspect raw bytes for the gzip magic.
	var entryPath string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".json") {
			entryPath = p
		}
		return nil
	})
	if entryPath == "" {
		t.Fatalf("no cache entry written under %s", dir)
	}
	raw, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("read %s: %v", entryPath, err)
	}
	if len(raw) < 2 || !bytes.Equal(raw[:2], []byte{0x1f, 0x8b}) {
		t.Fatalf("expected gzip magic at start of %s, got %x...", entryPath, raw[:min(8, len(raw))])
	}
	// Sanity: gzipped size should be materially smaller than the raw body.
	if len(raw) >= len(body) {
		t.Fatalf("gzip did not shrink: raw=%d, on-disk=%d", len(body), len(raw))
	}
}

func TestCachingTransport_ReadsLegacyNonGzippedEntry(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     ModeReadThrough,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Manually write a legacy plain-JSON entry at the path the transport
	// will compute for the given (exchange, method, url, principal).
	exchange := "lighter"
	url := "https://example.com/api/v1/trades?legacy=1"
	principal := "p-legacy"
	body := `{"legacy":"yes"}`

	c := tr.(*caching)
	path := c.entryPath(exchange, "GET", redactSecrets(url), principal, nil)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := entry{
		Exchange:  exchange,
		URL:       url,
		Method:    "GET",
		FetchedAt: time.Now(),
		BodyBytes: body,
		// Compressed left false / omitted, mirroring pre-rollout writes.
	}
	data, err := json.Marshal(&legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	// Live fetcher must NOT be invoked — the transport should read the
	// legacy plain-JSON entry and return its body verbatim.
	called := false
	got, err := tr.Get(context.Background(), exchange, url, principal, func(context.Context) ([]byte, error) {
		called = true
		return []byte("FROM_LIVE"), nil
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if called {
		t.Fatalf("live fetcher was called; legacy entry should have hit")
	}
	if string(got) != body {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestCachingTransport_RespectsPrincipalAllowlist(t *testing.T) {
	dir := t.TempDir()
	tr, err := New(Config{
		Mode:                     ModeReadThrough,
		Dir:                      dir,
		LiveTTL:                  time.Hour,
		CacheableForeverPatterns: DefaultCacheableForeverPatterns,
		PrincipalAllowlist:       map[string]bool{"alice": true},
		Clock:                    time.Now,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	url := "https://example.com/api/v1/trades?account_index=1"

	// alice: in allowlist -> cached. Second call must HIT.
	aliceCalls := 0
	aliceFetch := func(context.Context) ([]byte, error) {
		aliceCalls++
		return []byte(`{"who":"alice"}`), nil
	}
	if _, err := tr.Get(ctx, "lighter", url, "alice", aliceFetch); err != nil {
		t.Fatalf("alice Get1: %v", err)
	}
	if _, err := tr.Get(ctx, "lighter", url, "alice", aliceFetch); err != nil {
		t.Fatalf("alice Get2: %v", err)
	}
	if aliceCalls != 1 {
		t.Fatalf("alice live calls = %d, want 1 (second should hit)", aliceCalls)
	}

	// bob: NOT in allowlist -> bypass. Both calls must invoke live and
	// no file must land on disk for bob.
	bobCalls := 0
	bobFetch := func(context.Context) ([]byte, error) {
		bobCalls++
		return []byte(`{"who":"bob"}`), nil
	}
	if _, err := tr.Get(ctx, "lighter", url, "bob", bobFetch); err != nil {
		t.Fatalf("bob Get1: %v", err)
	}
	if _, err := tr.Get(ctx, "lighter", url, "bob", bobFetch); err != nil {
		t.Fatalf("bob Get2: %v", err)
	}
	if bobCalls != 2 {
		t.Fatalf("bob live calls = %d, want 2 (bypass; no caching)", bobCalls)
	}

	// Confirm exactly ONE entry exists (alice's). Counting files is the
	// simplest way to verify bob produced no on-disk write.
	count := 0
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".json") {
			count++
		}
		return nil
	})
	if count != 1 {
		t.Fatalf("on-disk entries = %d, want 1 (alice only)", count)
	}
}

func TestParsePrincipalAllowlist(t *testing.T) {
	cases := []struct {
		in   string
		want map[string]bool
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil},
		{"alice", map[string]bool{"alice": true}},
		{"alice,bob", map[string]bool{"alice": true, "bob": true}},
		{" alice , bob , ", map[string]bool{"alice": true, "bob": true}},
	}
	for _, tc := range cases {
		got := parsePrincipalAllowlist(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("parsePrincipalAllowlist(%q) size = %d, want %d", tc.in, len(got), len(tc.want))
			continue
		}
		for k := range tc.want {
			if !got[k] {
				t.Errorf("parsePrincipalAllowlist(%q) missing %q", tc.in, k)
			}
		}
	}
}

// TestCachingTransport_SolanaDexTransactionsRequiresBeforeCursor is the
// regression test for the credit-audit cache-correctness bug (task #55):
// Helius's /v0/addresses/<addr>/transactions endpoint is paginated by a
// `&before=<sig>` cursor. CURSOR-ANCHORED pages are immutable and may be
// cached forever; the FIRST PAGE (no `&before=` in the URL) is the
// newest-data window and MUST NOT be pinned because new transactions
// arriving later would be permanently invisible to incremental sync.
//
// Earlier the allowlist for "solana_dex" matched on the substring
// `/transactions` alone, which pinned the first page forever. After the
// fix the substring is `&before=` so only cursor-anchored pages are
// historical; the first page falls back to LiveTTL and refreshes each
// cycle.
func TestCachingTransport_SolanaDexTransactionsRequiresBeforeCursor(t *testing.T) {
	now := time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	tr := newTestTransport(t, ModeReadThrough, time.Hour, func() time.Time { return clock() })
	ctx := context.Background()

	// (1) FIRST PAGE — no `&before=`. Must NOT be treated as historical:
	// after LiveTTL elapses the live fetcher must be re-invoked.
	{
		firstPageURL := "https://api.helius.xyz/v0/addresses/WalletXYZ/transactions?api-key=REDACTED&limit=100"
		calls := 0
		live := func(context.Context) ([]byte, error) {
			calls++
			return []byte(`[{"signature":"s1"}]`), nil
		}
		if _, err := tr.Get(ctx, "solana_dex", firstPageURL, "WalletXYZ", live); err != nil {
			t.Fatalf("first-page initial Get: %v", err)
		}
		// Advance past LiveTTL — a non-historical entry MUST expire.
		now = now.Add(2 * time.Hour)
		if _, err := tr.Get(ctx, "solana_dex", firstPageURL, "WalletXYZ", live); err != nil {
			t.Fatalf("first-page re-Get after TTL: %v", err)
		}
		if calls != 2 {
			t.Fatalf("first page (no &before=) must not be cached forever: live calls = %d, want 2", calls)
		}
	}

	// (2) CURSOR PAGE — URL contains `&before=<sig>`. Must be treated as
	// historical: the live fetcher is invoked exactly once even after the
	// TTL has elapsed.
	{
		cursorPageURL := "https://api.helius.xyz/v0/addresses/WalletXYZ/transactions?api-key=REDACTED&limit=100&before=sigABC123"
		calls := 0
		live := func(context.Context) ([]byte, error) {
			calls++
			return []byte(`[{"signature":"s2"}]`), nil
		}
		if _, err := tr.Get(ctx, "solana_dex", cursorPageURL, "WalletXYZ", live); err != nil {
			t.Fatalf("cursor-page initial Get: %v", err)
		}
		// Advance well past LiveTTL (a year). Historical entry MUST still hit.
		now = now.Add(365 * 24 * time.Hour)
		if _, err := tr.Get(ctx, "solana_dex", cursorPageURL, "WalletXYZ", live); err != nil {
			t.Fatalf("cursor-page re-Get after a year: %v", err)
		}
		if calls != 1 {
			t.Fatalf("cursor-anchored page must be cached forever: live calls = %d, want 1", calls)
		}
	}
}

