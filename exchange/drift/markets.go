package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

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

// newMarketCache creates a new market cache with the given TTL
func newMarketCache(ttl time.Duration) *marketCache {
	return &marketCache{
		perpMarkets: make(map[int]MarketInfo),
		spotMarkets: make(map[int]MarketInfo),
		ttl:         ttl,
	}
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

	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/stats/markets", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
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

	// Build market maps
	perpMarkets := make(map[int]MarketInfo)
	spotMarkets := make(map[int]MarketInfo)

	for _, m := range response.Markets {
		info := MarketInfo{
			MarketIndex: m.MarketIndex,
			Symbol:      m.Symbol,
			BaseAsset:   m.BaseAsset,
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

// getMarketInfo returns market information for the given market index and type
// It will fetch from API if cache is expired or missing
func (c *Client) getMarketInfo(ctx context.Context, marketIndex int, marketType string) (MarketInfo, error) {
	// Try cache first
	if info, ok := c.marketCache.getMarket(marketIndex, marketType); ok {
		return info, nil
	}

	// Fetch markets if cache miss or expired
	if err := c.fetchMarkets(ctx); err != nil {
		// Return a fallback if we can't fetch markets
		// This allows the system to continue working even if markets API is down
		return MarketInfo{
			MarketIndex: marketIndex,
			Symbol:      fmt.Sprintf("UNKNOWN-%d", marketIndex),
			BaseAsset:   fmt.Sprintf("UNKNOWN-%d", marketIndex),
			QuoteAsset:  "USDC",
			MarketType:  marketType,
		}, nil
	}

	// Try cache again after fetch
	if info, ok := c.marketCache.getMarket(marketIndex, marketType); ok {
		return info, nil
	}

	// Market not found even after fetch - return fallback
	return MarketInfo{
		MarketIndex: marketIndex,
		Symbol:      fmt.Sprintf("UNKNOWN-%d", marketIndex),
		BaseAsset:   fmt.Sprintf("UNKNOWN-%d", marketIndex),
		QuoteAsset:  "USDC",
		MarketType:  marketType,
	}, nil
}
