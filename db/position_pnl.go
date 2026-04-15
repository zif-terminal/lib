package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// PositionPnLInput type alias
type PositionPnLInput = models.PositionPnLInput

// MissingPositionPnL type alias
type MissingPositionPnL = models.MissingPositionPnL

// ListMissingPositionPnL returns closed positions that are missing PnL for supported denominations.
// Results are limited by the limit parameter to support batch processing.
func (c *Client) ListMissingPositionPnL(ctx context.Context, limit int) ([]*MissingPositionPnL, error) {
	query := `
		query ListMissingPositionPnL($limit: Int!) {
			missing_position_pnl(limit: $limit) {
				position_id
				denomination
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"limit": limit,
	})

	var resp struct {
		MissingPositionPnL []*MissingPositionPnL `json:"missing_position_pnl"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list missing position pnl: %w", err)
	}

	return resp.MissingPositionPnL, nil
}

// AddPositionPnL batch-inserts position PnL records. Uses on_conflict to update existing rows.
// Inputs are written in chunks of 1000 to avoid Hasura payload limits.
func (c *Client) AddPositionPnL(ctx context.Context, inputs []*PositionPnLInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	const chunkSize = 1000
	totalAffected := 0

	for start := 0; start < len(inputs); start += chunkSize {
		end := start + chunkSize
		if end > len(inputs) {
			end = len(inputs)
		}
		chunk := inputs[start:end]

		affected, err := c.addPositionPnLChunk(ctx, chunk)
		if err != nil {
			return totalAffected, err
		}
		totalAffected += affected
	}

	return totalAffected, nil
}

func (c *Client) addPositionPnLChunk(ctx context.Context, inputs []*PositionPnLInput) (int, error) {
	query := `
		mutation AddPositionPnL($objects: [position_pnl_insert_input!]!) {
			insert_position_pnl(
				objects: $objects
				on_conflict: {
					constraint: position_pnl_position_id_denomination_key
					update_columns: [realized_pnl, trade_pnl, funding_pnl, fee_pnl, interest_pnl]
				}
			) {
				affected_rows
			}
		}
	`

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"position_id":  input.PositionID.String(),
			"denomination": input.Denomination,
			"realized_pnl": input.RealizedPnL,
			"trade_pnl":    input.TradePnL,
			"funding_pnl":  input.FundingPnL,
			"fee_pnl":      input.FeePnL,
			"interest_pnl": input.InterestPnL,
		}
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertPositionPnL struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_position_pnl"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add position pnl: %w", err)
	}

	return resp.InsertPositionPnL.AffectedRows, nil
}

// GetPositionsByIDs fetches positions by their IDs. Returns all matching positions.
func (c *Client) GetPositionsByIDs(ctx context.Context, ids []uuid.UUID) ([]*Position, error) {
	if len(ids) == 0 {
		return []*Position{}, nil
	}

	query := `
		query GetPositionsByIDs($ids: [uuid!]!) {
			positions(where: { id: { _in: $ids } }) {
				id
				exchange_account_id
				market
				market_type
				side
				status
				quantity
				start_time
				end_time
				updated_at
			}
		}
	`

	idStrings := make([]string, len(ids))
	for i, id := range ids {
		idStrings[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"ids": idStrings,
	})

	var resp struct {
		Positions []*Position `json:"positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get positions by IDs: %w", err)
	}

	return resp.Positions, nil
}

// GetEventValuesByEventIDs fetches event values for the given event IDs and denomination.
// Returns a map keyed by a composite of (event_id, event_type) for quick lookup.
func (c *Client) GetEventValuesByEventIDs(ctx context.Context, eventIDs []uuid.UUID, denomination string) ([]*EventValue, error) {
	if len(eventIDs) == 0 {
		return []*EventValue{}, nil
	}

	query := `
		query GetEventValuesByEventIDs($event_ids: [uuid!]!, $denomination: String!) {
			event_values(where: {
				event_id: { _in: $event_ids }
				denomination: { _eq: $denomination }
			}) {
				id
				event_id
				event_type
				denomination
				quantity
			}
		}
	`

	idStrings := make([]string, len(eventIDs))
	for i, id := range eventIDs {
		idStrings[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"event_ids":    idStrings,
		"denomination": denomination,
	})

	var resp struct {
		EventValues []*EventValue `json:"event_values"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get event values by event IDs: %w", err)
	}

	return resp.EventValues, nil
}
