package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// parseBaseAssetFromSymbol extracts base asset from symbol
// e.g., "SOL-PERP" -> "SOL"
func parseBaseAssetFromSymbol(symbol string) string {
	parts := strings.Split(symbol, "-")
	if len(parts) > 0 {
		return parts[0]
	}
	return symbol
}

// MarketInfo contains parsed market information
type MarketInfo struct {
	MarketIndex int
	Symbol      string
	BaseAsset   string
	QuoteAsset  string
	MarketType  string
}

// marketCache caches market information with TTL
type marketCache struct {
	perpMarkets map[int]MarketInfo
	spotMarkets map[int]MarketInfo
	mu          sync.RWMutex
	lastFetch   time.Time
	ttl         time.Duration
}

// newMarketCache creates a new market cache with the given TTL,
// pre-seeded with hardcoded Drift mainnet market mappings from
// https://github.com/drift-labs/protocol-v2/blob/master/sdk/src/constants/
func newMarketCache(ttl time.Duration) *marketCache {
	mc := &marketCache{
		perpMarkets: make(map[int]MarketInfo),
		spotMarkets: make(map[int]MarketInfo),
		ttl:         ttl,
	}

	// Hardcoded perp markets (from protocol-v2/sdk/src/constants/perpMarkets.ts)
	perpDefaults := map[int]string{
		0: "SOL", 1: "BTC", 2: "ETH", 3: "APT", 4: "1MBONK", 5: "POL",
		6: "ARB", 7: "DOGE", 8: "BNB", 9: "SUI", 10: "1MPEPE", 11: "OP",
		12: "RENDER", 13: "XRP", 14: "HNT", 15: "INJ", 16: "LINK", 17: "RLB",
		18: "PYTH", 19: "TIA", 20: "JTO", 21: "SEI", 22: "AVAX", 23: "WIF",
		24: "JUP", 25: "DYM", 26: "TAO", 27: "W", 28: "KMNO", 29: "TNSR",
		30: "DRIFT", 31: "CLOUD", 32: "IO", 33: "ZEX", 34: "POPCAT",
		44: "MOTHER", 45: "MOODENG", 59: "HYPE", 60: "LTC", 61: "ME",
		62: "PENGU", 63: "AI16Z", 64: "TRUMP", 65: "MELANIA", 66: "BERA",
		69: "KAITO", 70: "IP", 71: "FARTCOIN", 72: "ADA", 73: "PAXG",
		74: "LAUNCHCOIN", 75: "PUMP", 76: "ASTER", 77: "XPL", 78: "2Z",
		79: "ZEC", 80: "MNT", 81: "1KPUMP", 82: "MET", 83: "1KMON",
		84: "LIT", 85: "BP",
	}
	for idx, base := range perpDefaults {
		mc.perpMarkets[idx] = MarketInfo{
			MarketIndex: idx,
			Symbol:      base + "-PERP",
			BaseAsset:   base,
			QuoteAsset:  "USDC",
			MarketType:  "perp",
		}
	}

	// Hardcoded spot markets (from protocol-v2/sdk/src/constants/spotMarkets.ts)
	spotDefaults := map[int]string{
		0: "USDC", 1: "SOL", 2: "mSOL", 3: "wBTC", 4: "wETH", 5: "USDT",
		6: "jitoSOL", 7: "PYTH", 8: "bSOL", 9: "JTO", 10: "WIF", 11: "JUP",
		12: "RENDER", 13: "W", 14: "TNSR", 15: "DRIFT", 16: "INF", 17: "dSOL",
		18: "USDY", 19: "JLP", 20: "POPCAT", 21: "CLOUD", 25: "BNSOL",
		26: "MOTHER", 27: "cbBTC", 29: "META", 30: "ME", 31: "PENGU",
		32: "BONK", 35: "AI16Z", 36: "TRUMP", 37: "MELANIA", 39: "FARTCOIN",
		40: "JitoSOL-3", 41: "PT-fragSOL-10JUL25-3", 42: "PT-kySOL-15JUN25-3",
		43: "PT-dSOL-30JUN25-3", 44: "JTO-3", 45: "zBTC", 46: "ZEUS",
		47: "USDC-4", 48: "USDT-4", 49: "SOL-2", 50: "JitoSOL-2", 51: "JTO-2",
		52: "dfdvSOL", 53: "sACRED", 54: "EURC", 55: "PT-fragSOL-31OCT25-3",
		56: "PUMP", 57: "syrupUSDC", 58: "LBTC", 59: "2Z", 60: "MET",
		61: "CASH", 62: "USD1",
	}
	for idx, base := range spotDefaults {
		mc.spotMarkets[idx] = MarketInfo{
			MarketIndex: idx,
			Symbol:      base,
			BaseAsset:   base,
			QuoteAsset:  "USDC",
			MarketType:  "spot",
		}
	}

	return mc
}

// isExpired checks if the cache has expired
func (mc *marketCache) isExpired() bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return time.Since(mc.lastFetch) > mc.ttl
}

// getMarket returns market info for the given index and type
func (mc *marketCache) getMarket(marketIndex int, marketType string) (MarketInfo, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	var markets map[int]MarketInfo
	if marketType == "perp" {
		markets = mc.perpMarkets
	} else {
		markets = mc.spotMarkets
	}

	info, ok := markets[marketIndex]
	return info, ok
}

// setMarkets updates the cache with new market data
func (mc *marketCache) setMarkets(perpMarkets, spotMarkets map[int]MarketInfo) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.perpMarkets = perpMarkets
	mc.spotMarkets = spotMarkets
	mc.lastFetch = time.Now()
}

// fetchMarkets fetches market information from the Drift API
func (c *Client) fetchMarkets(ctx context.Context) error {
	// Check if cache is still valid
	if !c.marketCache.isExpired() {
		return nil
	}

	resp, err := c.doRequestWithRetry(ctx, c.baseURL+"/stats/markets")
	if err != nil {
		return fmt.Errorf("failed to fetch markets: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("markets API returned status %d", resp.StatusCode)
	}

	var response driftMarketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return fmt.Errorf("failed to decode markets response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("markets API returned success=false")
	}

	// If API returned no markets, don't overwrite hardcoded defaults.
	// The hardcoded defaults are a legitimate fallback (keeps us running),
	// but an empty response from the markets API is a real upstream regression
	// that degrades our ability to resolve newly-added markets — log loudly.
	if len(response.Markets) == 0 {
		log.Printf("ERROR drift/markets: API returned 0 markets — relying on hardcoded defaults (newly-added markets will fail to resolve) | url=%s", c.baseURL+"/stats/markets")
		// Still mark as fetched so we don't retry immediately
		c.marketCache.mu.Lock()
		c.marketCache.lastFetch = time.Now()
		c.marketCache.mu.Unlock()
		return nil
	}

	// Build market maps
	perpMarkets := make(map[int]MarketInfo)
	spotMarkets := make(map[int]MarketInfo)

	for _, m := range response.Markets {
		// For spot markets, symbol IS the asset (e.g., "SOL", "USDC")
		// For perp markets, symbol is like "SOL-PERP" and baseAsset is "SOL"
		baseAsset := m.BaseAsset
		if baseAsset == "" {
			// Fallback: for spot markets, use symbol directly
			// For perp markets, strip "-PERP" suffix
			if m.MarketType == "spot" {
				baseAsset = m.Symbol
			} else {
				baseAsset = parseBaseAssetFromSymbol(m.Symbol)
			}
		}

		info := MarketInfo{
			MarketIndex: m.MarketIndex,
			Symbol:      m.Symbol,
			BaseAsset:   baseAsset,
			QuoteAsset:  "USDC", // Drift uses USDC as quote for all markets
			MarketType:  m.MarketType,
		}

		if m.MarketType == "perp" {
			perpMarkets[m.MarketIndex] = info
		} else {
			spotMarkets[m.MarketIndex] = info
		}
	}

	c.marketCache.setMarkets(perpMarkets, spotMarkets)
	return nil
}

// ensureMarketCache pre-warms the market cache if it's empty or expired.
// Call this before processing a batch of records to avoid per-record API calls.
func (c *Client) ensureMarketCache(ctx context.Context) error {
	if c.marketCache.isExpired() {
		return c.fetchMarkets(ctx)
	}
	return nil
}

// getMarketInfo returns market information for the given market index and type.
// It will fetch from API if cache is expired or missing.
// Returns an error if the market cannot be resolved — callers should skip the
// record rather than persist UNKNOWN data.
func (c *Client) getMarketInfo(ctx context.Context, marketIndex int, marketType string) (MarketInfo, error) {
	// Try cache first
	if info, ok := c.marketCache.getMarket(marketIndex, marketType); ok {
		return info, nil
	}

	// Fetch markets if cache miss or expired
	if err := c.fetchMarkets(ctx); err != nil {
		return MarketInfo{}, fmt.Errorf("failed to resolve market %s index %d: %w", marketType, marketIndex, err)
	}

	// Try cache again after fetch
	if info, ok := c.marketCache.getMarket(marketIndex, marketType); ok {
		return info, nil
	}

	// Market not found even after fetch
	return MarketInfo{}, fmt.Errorf("market %s index %d not found in Drift markets API", marketType, marketIndex)
}
