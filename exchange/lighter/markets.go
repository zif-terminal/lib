package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// marketInfo holds resolved market metadata for a given market_id.
type marketInfo struct {
	Symbol     string
	BaseAsset  string
	QuoteAsset string
	MarketType string // "perp" or "spot"
}

// marketCache caches market_id -> marketInfo mappings.
// It is populated lazily on first use and shared across all Client instances.
type marketCache struct {
	mu      sync.RWMutex
	markets map[int]*marketInfo
	loaded  bool
}

var globalMarketCache = &marketCache{
	markets: make(map[int]*marketInfo),
}

// get returns the market info for a given market_id, loading the cache if needed.
func (mc *marketCache) get(ctx context.Context, c *Client, marketID int) (*marketInfo, error) {
	mc.mu.RLock()
	if mc.loaded {
		info, ok := mc.markets[marketID]
		mc.mu.RUnlock()
		if ok {
			return info, nil
		}
		// Market not in cache — force reload in case new markets were added
		return mc.reload(ctx, c, marketID)
	}
	mc.mu.RUnlock()

	return mc.reload(ctx, c, marketID)
}

// reload fetches the full market list from the API and updates the cache.
func (mc *marketCache) reload(ctx context.Context, c *Client, marketID int) (*marketInfo, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Double-check after acquiring write lock
	if mc.loaded {
		if info, ok := mc.markets[marketID]; ok {
			return info, nil
		}
	}

	url := c.baseURL + "/orderBookDetails"
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch order book details: %w", err)
	}

	if body == nil {
		return nil, fmt.Errorf("no order book details returned")
	}

	var resp lighterOrderBookDetailsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode order book details: %w", err)
	}

	for _, ob := range resp.OrderBooks {
		mc.markets[ob.MarketID] = &marketInfo{
			Symbol:     ob.Symbol,
			BaseAsset:  ob.BaseAsset,
			QuoteAsset: ob.QuoteAsset,
			MarketType: ob.MarketType,
		}
	}
	mc.loaded = true

	info, ok := mc.markets[marketID]
	if !ok {
		return nil, fmt.Errorf("unknown market_id %d", marketID)
	}
	return info, nil
}

// resolveMarket resolves a market_id to its metadata using the global cache.
func (c *Client) resolveMarket(ctx context.Context, marketID int) (*marketInfo, error) {
	return globalMarketCache.get(ctx, c, marketID)
}

// resetMarketCache resets the global market cache. Used in tests.
func resetMarketCache() {
	globalMarketCache.mu.Lock()
	defer globalMarketCache.mu.Unlock()
	globalMarketCache.markets = make(map[int]*marketInfo)
	globalMarketCache.loaded = false
}
