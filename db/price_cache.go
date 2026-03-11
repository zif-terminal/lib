package db

import (
	"context"
	"fmt"
	"time"

	"github.com/zif-terminal/lib/models"
)

// PriceCache type aliases
type PriceCache = models.PriceCache
type PriceCacheInput = models.PriceCacheInput

// GetCachedPrice looks up a cached price within a time tolerance window.
// Returns the closest price entry within [timestamp-tolerance, timestamp+tolerance],
// or nil if no match is found.
func (c *Client) GetCachedPrice(ctx context.Context, asset, denomination string, timestamp time.Time, tolerance time.Duration) (*PriceCache, error) {
	lower := timestamp.Add(-tolerance)
	upper := timestamp.Add(tolerance)

	query := `
		query GetCachedPrice($asset: String!, $denomination: String!, $lower: timestamptz!, $upper: timestamptz!) {
			price_cache(
				where: {
					asset: { _eq: $asset }
					denomination: { _eq: $denomination }
					timestamp: { _gte: $lower, _lte: $upper }
				}
				order_by: { timestamp: asc }
				limit: 1
			) {
				id
				asset
				denomination
				timestamp
				price
				source
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"asset":        asset,
		"denomination": denomination,
		"lower":        lower.Format(time.RFC3339Nano),
		"upper":        upper.Format(time.RFC3339Nano),
	})

	var resp struct {
		PriceCache []*PriceCache `json:"price_cache"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get cached price: %w", err)
	}

	if len(resp.PriceCache) == 0 {
		return nil, nil
	}

	return resp.PriceCache[0], nil
}

// AddPriceCache stores a resolved price in the cache.
// Uses on_conflict to avoid duplicates on (asset, denomination, timestamp, source).
func (c *Client) AddPriceCache(ctx context.Context, input *PriceCacheInput) error {
	query := `
		mutation AddPriceCache($object: price_cache_insert_input!) {
			insert_price_cache_one(
				object: $object
				on_conflict: {
					constraint: price_cache_asset_denomination_timestamp_source_key
					update_columns: [price]
				}
			) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"object": map[string]interface{}{
			"asset":        input.Asset,
			"denomination": input.Denomination,
			"timestamp":    input.Timestamp.Format(time.RFC3339Nano),
			"price":        input.Price,
			"source":       input.Source,
		},
	})

	var resp struct {
		InsertPriceCacheOne *struct {
			ID string `json:"id"`
		} `json:"insert_price_cache_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to add price cache: %w", err)
	}

	return nil
}
