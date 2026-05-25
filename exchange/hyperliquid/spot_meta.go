package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// hlSpotMetaResponse is the response from the spotMeta endpoint.
// Request: {"type": "spotMeta"}
type hlSpotMetaResponse struct {
	Tokens []hlSpotToken    `json:"tokens"`
	Universe []hlSpotMarket `json:"universe"`
}

// hlSpotAssetCtx is a single entry from the spotMetaAndAssetCtxs second slot.
// We only need the market identifier and the mark price; the rest of the
// payload (24h volumes, supply, prevDay) is irrelevant to balance snapshots.
type hlSpotAssetCtx struct {
	Coin   string `json:"coin"`   // e.g. "PURR/USDC", "@1", "@107"
	MarkPx string `json:"markPx"` // e.g. "10.101"
	MidPx  string `json:"midPx"`  // fallback when markPx is empty/zero
}

// hlSpotToken represents a token in the spotMeta response.
type hlSpotToken struct {
	Name        string `json:"name"`
	Index       int    `json:"index"`
	SzDecimals  int    `json:"szDecimals"`
	WeiDecimals int    `json:"weiDecimals"`
	TokenID     string `json:"tokenId"`
	IsCanonical bool   `json:"isCanonical"`
}

// hlSpotMarket represents a spot trading pair in the spotMeta universe.
type hlSpotMarket struct {
	Name        string `json:"name"`
	Tokens      []int  `json:"tokens"` // [baseTokenIndex, quoteTokenIndex]
	Index       int    `json:"index"`
	IsCanonical bool   `json:"isCanonical"`
}

// spotMetaCache caches the mapping from @N universe index to token names.
// Populated lazily on first use and shared across all Client instances.
type spotMetaCache struct {
	mu     sync.RWMutex
	// universeIndexToBase maps universe index N (from @N) to the base token name
	universeIndexToBase map[int]string
	// universeIndexToQuote maps universe index N to the quote token name
	universeIndexToQuote map[int]string
	loaded bool
}

var globalSpotMetaCache = &spotMetaCache{
	universeIndexToBase:  make(map[int]string),
	universeIndexToQuote: make(map[int]string),
}

// resolveSpotCoin resolves a @N spot coin index to the actual token name.
// If the coin doesn't start with @, it is returned as-is.
func (mc *spotMetaCache) resolveSpotCoin(ctx context.Context, c *Client, coin string) (base, quote string, err error) {
	if len(coin) < 2 || coin[0] != '@' {
		// Not an indexed coin — return as-is with USDC quote
		return coin, "USDC", nil
	}

	var idx int
	if _, err := fmt.Sscanf(coin, "@%d", &idx); err != nil {
		return coin, "USDC", nil
	}

	mc.mu.RLock()
	if mc.loaded {
		base, baseOK := mc.universeIndexToBase[idx]
		quote, quoteOK := mc.universeIndexToQuote[idx]
		mc.mu.RUnlock()
		if baseOK && quoteOK {
			return base, quote, nil
		}
		// Index not found — try reloading in case new markets were added
		return mc.reload(ctx, c, idx)
	}
	mc.mu.RUnlock()

	return mc.reload(ctx, c, idx)
}

// reload fetches spot metadata from the API and rebuilds the cache.
func (mc *spotMetaCache) reload(ctx context.Context, c *Client, idx int) (string, string, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Double-check after acquiring write lock
	if mc.loaded {
		if base, ok := mc.universeIndexToBase[idx]; ok {
			return base, mc.universeIndexToQuote[idx], nil
		}
	}

	var resp hlSpotMetaResponse
	if err := c.doPost(ctx, map[string]string{"type": "spotMeta"}, &resp); err != nil {
		return "", "", fmt.Errorf("failed to fetch spot metadata: %w", err)
	}

	// Build token index -> name lookup
	tokenNames := make(map[int]string, len(resp.Tokens))
	for _, token := range resp.Tokens {
		tokenNames[token.Index] = token.Name
	}

	// Build universe index -> base/quote token names
	for _, market := range resp.Universe {
		if len(market.Tokens) >= 2 {
			baseName := tokenNames[market.Tokens[0]]
			quoteName := tokenNames[market.Tokens[1]]
			mc.universeIndexToBase[market.Index] = baseName
			mc.universeIndexToQuote[market.Index] = quoteName
		}
	}
	mc.loaded = true

	base, baseOK := mc.universeIndexToBase[idx]
	quote, quoteOK := mc.universeIndexToQuote[idx]
	if !baseOK || !quoteOK {
		return "", "", fmt.Errorf("unknown spot market index @%d", idx)
	}
	return base, quote, nil
}

// resolveSpotCoin resolves a @N spot coin to the actual token name using the global cache.
func (c *Client) resolveSpotCoin(ctx context.Context, coin string) (base, quote string, err error) {
	return globalSpotMetaCache.resolveSpotCoin(ctx, c, coin)
}

// fetchSpotMarkPrices fetches current mark prices for every spot market quoted
// in USDC, keyed by base-token NAME (e.g. "HYPE", "PURR"). Used by
// FetchBalances to attach an oracle_price + usd_value to each non-USDC spot
// snapshot row. USDC itself is implicitly priced at 1.0 by the caller.
//
// Hyperliquid's spotMetaAndAssetCtxs response is a 2-tuple
// [meta, ctxs] where ctxs are keyed by market NAME (e.g. "PURR/USDC", "@1",
// "@107"). We unify the two halves here:
//
//   meta.universe[i].name      = "@107"
//   meta.universe[i].tokens    = [baseTokenIdx, quoteTokenIdx]
//   meta.tokens[baseTokenIdx]  = {name: "HYPE", index: 150, ...}
//
// Only markets quoted in USDC (token index 0) are returned — non-USDC markets
// would price the asset against another token, not USD, and that's not what
// the snapshot column is meant to express.
//
// Tokens with multiple USDC-quoted markets (rare) keep the last one
// encountered; the API doesn't currently expose duplicates so this is purely
// defensive.
func (c *Client) fetchSpotMarkPrices(ctx context.Context) (map[string]string, error) {
	// The HL response is a heterogeneous 2-tuple [meta, ctxs] — use raw
	// json.RawMessage to decode each slot with its own type.
	var raw []json.RawMessage
	if err := c.doPost(ctx, map[string]string{"type": "spotMetaAndAssetCtxs"}, &raw); err != nil {
		return nil, fmt.Errorf("failed to fetch spot mark prices: %w", err)
	}
	if len(raw) != 2 {
		return nil, fmt.Errorf("spotMetaAndAssetCtxs returned %d entries, expected 2", len(raw))
	}

	var meta hlSpotMetaResponse
	if err := json.Unmarshal(raw[0], &meta); err != nil {
		return nil, fmt.Errorf("failed to decode spot meta: %w", err)
	}

	var ctxs []hlSpotAssetCtx
	if err := json.Unmarshal(raw[1], &ctxs); err != nil {
		return nil, fmt.Errorf("failed to decode spot asset ctxs: %w", err)
	}

	// tokenIdx -> token name
	tokenNames := make(map[int]string, len(meta.Tokens))
	const usdcTokenIdx = 0
	for _, t := range meta.Tokens {
		tokenNames[t.Index] = t.Name
	}

	// market name (e.g. "@107") -> base token name, only when quote == USDC.
	marketToBase := make(map[string]string, len(meta.Universe))
	for _, m := range meta.Universe {
		if len(m.Tokens) < 2 {
			continue
		}
		if m.Tokens[1] != usdcTokenIdx {
			// Non-USDC quote — skip; we only want USDC-denominated marks.
			continue
		}
		baseName, ok := tokenNames[m.Tokens[0]]
		if !ok || baseName == "" {
			continue
		}
		marketToBase[m.Name] = baseName
	}

	// Build the result.
	prices := make(map[string]string, len(ctxs))
	for _, c := range ctxs {
		base, ok := marketToBase[c.Coin]
		if !ok {
			continue // not a USDC-quoted market or unknown
		}
		px := c.MarkPx
		if px == "" || px == "0" || px == "0.0" {
			px = c.MidPx
		}
		if px == "" || px == "0" || px == "0.0" {
			continue // no usable price
		}
		prices[base] = px
	}
	return prices, nil
}

// resetSpotMetaCache resets the global spot metadata cache. Used in tests.
func resetSpotMetaCache() {
	globalSpotMetaCache.mu.Lock()
	defer globalSpotMetaCache.mu.Unlock()
	globalSpotMetaCache.universeIndexToBase = make(map[int]string)
	globalSpotMetaCache.universeIndexToQuote = make(map[int]string)
	globalSpotMetaCache.loaded = false
}
