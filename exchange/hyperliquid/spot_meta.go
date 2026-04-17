package hyperliquid

import (
	"context"
	"fmt"
	"sync"
)

// hlSpotMetaResponse is the response from the spotMeta endpoint.
// Request: {"type": "spotMeta"}
type hlSpotMetaResponse struct {
	Tokens []hlSpotToken    `json:"tokens"`
	Universe []hlSpotMarket `json:"universe"`
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

// resetSpotMetaCache resets the global spot metadata cache. Used in tests.
func resetSpotMetaCache() {
	globalSpotMetaCache.mu.Lock()
	defer globalSpotMetaCache.mu.Unlock()
	globalSpotMetaCache.universeIndexToBase = make(map[int]string)
	globalSpotMetaCache.universeIndexToQuote = make(map[int]string)
	globalSpotMetaCache.loaded = false
}
