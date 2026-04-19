package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

	// Process both perp and spot order books
	allBooks := append(resp.OrderBookDetails, resp.SpotOrderBooks...)
	for _, ob := range allBooks {
		base, quote := deriveBaseQuote(ob.Symbol, ob.MarketType)
		mc.markets[ob.MarketID] = &marketInfo{
			Symbol:     ob.Symbol,
			BaseAsset:  base,
			QuoteAsset: quote,
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

// deriveBaseQuote derives base and quote asset from a Lighter market symbol.
// Perp symbols are just the base asset (e.g. "ETH" -> ETH/USDC).
// Spot symbols use "BASE/QUOTE" format (e.g. "UNI/USDC").
func deriveBaseQuote(symbol, marketType string) (base, quote string) {
	if marketType == "spot" {
		parts := strings.SplitN(symbol, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	// Perp markets: symbol is just the base asset, quote is always USDC
	return symbol, "USDC"
}

// assetCache caches asset_id -> symbol mappings derived from spot order book details.
type assetCache struct {
	mu     sync.RWMutex
	assets map[int]string // asset_id -> symbol
	loaded bool
}

var globalAssetCache = &assetCache{
	assets: make(map[int]string),
}

// get returns the symbol for a given asset_id, loading the cache if needed.
func (ac *assetCache) get(ctx context.Context, c *Client, assetID int) (string, error) {
	ac.mu.RLock()
	if ac.loaded {
		sym, ok := ac.assets[assetID]
		ac.mu.RUnlock()
		if ok {
			return sym, nil
		}
		return "", fmt.Errorf("unknown asset_id %d", assetID)
	}
	ac.mu.RUnlock()

	return ac.reload(ctx, c, assetID)
}

// reload fetches the full market list from the API and builds the asset map.
func (ac *assetCache) reload(ctx context.Context, c *Client, assetID int) (string, error) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if ac.loaded {
		if sym, ok := ac.assets[assetID]; ok {
			return sym, nil
		}
		return "", fmt.Errorf("unknown asset_id %d", assetID)
	}

	url := c.baseURL + "/orderBookDetails"
	body, err := c.doGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch order book details: %w", err)
	}

	if body == nil {
		return "", fmt.Errorf("no order book details returned")
	}

	var resp lighterOrderBookDetailsResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to decode order book details: %w", err)
	}

	// Build asset_id -> symbol map from spot markets which have explicit base/quote
	for _, ob := range resp.SpotOrderBooks {
		parts := strings.SplitN(ob.Symbol, "/", 2)
		if len(parts) == 2 {
			ac.assets[ob.BaseAssetID] = parts[0]
			ac.assets[ob.QuoteAssetID] = parts[1]
		}
	}
	ac.loaded = true

	sym, ok := ac.assets[assetID]
	if !ok {
		return "", fmt.Errorf("unknown asset_id %d", assetID)
	}
	return sym, nil
}

// resolveMarket resolves a market_id to its metadata using the global cache.
func (c *Client) resolveMarket(ctx context.Context, marketID int) (*marketInfo, error) {
	return globalMarketCache.get(ctx, c, marketID)
}

// resolveAsset resolves an asset_id to its symbol using the global asset cache.
func (c *Client) resolveAsset(ctx context.Context, assetID int) (string, error) {
	return globalAssetCache.get(ctx, c, assetID)
}

// resetMarketCache resets the global market cache. Used in tests.
func resetMarketCache() {
	globalMarketCache.mu.Lock()
	defer globalMarketCache.mu.Unlock()
	globalMarketCache.markets = make(map[int]*marketInfo)
	globalMarketCache.loaded = false

	globalAssetCache.mu.Lock()
	defer globalAssetCache.mu.Unlock()
	globalAssetCache.assets = make(map[int]string)
	globalAssetCache.loaded = false
}
