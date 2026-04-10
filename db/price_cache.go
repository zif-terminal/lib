package db

import (
	"context"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// Price cache type aliases
type Price = models.Price
type PriceInput = models.PriceInput
type PriceRecord = models.PriceRecord

// AddPrices batch upserts price records into the price_cache table.
// On conflict (same asset, denomination, timestamp, source), the price is updated.
// Returns the number of affected rows.
func (c *Client) AddPrices(ctx context.Context, inputs []*PriceInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	// Deduplicate: PostgreSQL ON CONFLICT cannot affect the same row twice in one INSERT.
	// Keep the last price for each (asset, denomination, timestamp, source) key.
	type priceKey struct {
		Asset, Denomination, Source string
		TimestampMs                int64
	}
	seen := make(map[priceKey]int, len(inputs))
	deduped := make([]*PriceInput, 0, len(inputs))
	for _, input := range inputs {
		key := priceKey{input.Asset, input.Denomination, input.Source, input.TimestampMs}
		if idx, ok := seen[key]; ok {
			deduped[idx] = input // overwrite with latest
		} else {
			seen[key] = len(deduped)
			deduped = append(deduped, input)
		}
	}

	const batchSize = 500
	totalAffected := 0

	for start := 0; start < len(deduped); start += batchSize {
		end := start + batchSize
		if end > len(deduped) {
			end = len(deduped)
		}
		batch := deduped[start:end]

		affected, err := c.addPricesBatch(ctx, batch)
		if err != nil {
			return totalAffected, err
		}
		totalAffected += affected
	}

	return totalAffected, nil
}

func (c *Client) addPricesBatch(ctx context.Context, inputs []*PriceInput) (int, error) {
	query := `
		mutation AddPrices($objects: [price_cache_insert_input!]!) {
			insert_price_cache(
				objects: $objects
				on_conflict: {
					constraint: price_cache_asset_denomination_timestamp_source_key
					update_columns: [price]
				}
			) {
				affected_rows
			}
		}
	`

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"asset":        input.Asset,
			"denomination": input.Denomination,
			"timestamp":    input.TimestampMs,
			"price":        input.Price,
			"source":       input.Source,
		}
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertPriceCache struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_price_cache"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add prices: %w", err)
	}

	return resp.InsertPriceCache.AffectedRows, nil
}

// GetNearestPrice finds the closest price record within a tolerance window.
// It queries for prices where the timestamp is within ±toleranceMs of the given timestampMs,
// then picks the one closest to the target timestamp.
// Returns nil (not an error) if no price is found within the tolerance.
func (c *Client) GetNearestPrice(ctx context.Context, asset string, denomination string, timestampMs int64, toleranceMs int64) (*Price, error) {
	query := `
		query GetNearestPrice($asset: String!, $denomination: String!, $ts_min: bigint!, $ts_max: bigint!) {
			price_cache(
				where: {
					asset: { _eq: $asset }
					denomination: { _eq: $denomination }
					timestamp: { _gte: $ts_min, _lte: $ts_max }
				}
				order_by: { timestamp: asc }
			) {
				asset
				denomination
				timestamp
				price
				source
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"asset":        asset,
		"denomination": denomination,
		"ts_min":       timestampMs - toleranceMs,
		"ts_max":       timestampMs + toleranceMs,
	})

	var resp struct {
		PriceCache []*Price `json:"price_cache"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get nearest price: %w", err)
	}

	if len(resp.PriceCache) == 0 {
		return nil, nil
	}

	// Find the price closest to the target timestamp
	var closest *Price
	var minDist int64
	for _, p := range resp.PriceCache {
		dist := timestampMs - p.Timestamp
		if dist < 0 {
			dist = -dist
		}
		if closest == nil || dist < minDist {
			closest = p
			minDist = dist
		}
	}

	return closest, nil
}
