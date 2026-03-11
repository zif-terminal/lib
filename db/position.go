package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Position type aliases
type Position = models.Position
type PositionInput = models.PositionInput
type PositionEvent = models.PositionEvent
type PositionEventInput = models.PositionEventInput
type PositionFilter = models.PositionFilter

// DeletePositionsForAccount deletes all positions (and cascades to position_events)
// for a given account. Returns the number of deleted rows.
func (c *Client) DeletePositionsForAccount(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		mutation DeletePositionsForAccount($account_id: uuid!) {
			delete_positions(where: { exchange_account_id: { _eq: $account_id } }) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		DeletePositions struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete positions: %w", err)
	}

	return resp.DeletePositions.AffectedRows, nil
}

// DeleteOpenPositionsForAccount deletes only open positions (and cascades to their events)
// for a given account. Closed positions and their events are preserved.
func (c *Client) DeleteOpenPositionsForAccount(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		mutation DeleteOpenPositionsForAccount($account_id: uuid!) {
			delete_positions(where: {
				exchange_account_id: { _eq: $account_id }
				status: { _eq: "open" }
			}) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		DeletePositions struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete open positions: %w", err)
	}

	return resp.DeletePositions.AffectedRows, nil
}

// DeletePositionsByIDs deletes positions by their specific IDs (and cascades to position_events).
// Used by the write-first-then-delete pattern to remove old positions after new ones are inserted.
func (c *Client) DeletePositionsByIDs(ctx context.Context, ids []uuid.UUID) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	query := `
		mutation DeletePositionsByIDs($ids: [uuid!]!) {
			delete_positions(where: { id: { _in: $ids } }) {
				affected_rows
			}
		}
	`

	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"ids": idStrs,
	})

	var resp struct {
		DeletePositions struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete positions by IDs: %w", err)
	}

	return resp.DeletePositions.AffectedRows, nil
}

// CountClosedPositions returns the number of closed positions for an account.
func (c *Client) CountClosedPositions(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		query CountClosedPositions($account_id: uuid!) {
			positions_aggregate(where: {
				exchange_account_id: { _eq: $account_id }
				status: { _eq: "closed" }
			}) {
				aggregate { count }
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		PositionsAggregate struct {
			Aggregate struct {
				Count int `json:"count"`
			} `json:"aggregate"`
		} `json:"positions_aggregate"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to count closed positions: %w", err)
	}

	return resp.PositionsAggregate.Aggregate.Count, nil
}

// AddPositions batch-inserts positions and returns the created records (with IDs).
func (c *Client) AddPositions(ctx context.Context, inputs []*PositionInput) ([]*Position, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	query := `
		mutation AddPositions($objects: [positions_insert_input!]!) {
			insert_positions(objects: $objects) {
				returning {
					id
					exchange_account_id
					market
					market_type
					side
					status
					quantity
					entry_price
					exit_price
					total_fees
					cumulative_funding
					start_time
					end_time
					order_id
				}
			}
		}
	`

	objects := make([]map[string]interface{}, len(inputs))
	for i, inp := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": inp.ExchangeAccountID.String(),
			"market":              inp.Market,
			"market_type":         inp.MarketType,
			"side":                inp.Side,
			"status":              inp.Status,
			"quantity":            inp.Quantity,
			"entry_price":         inp.EntryPrice,
			"total_fees":          inp.TotalFees,
			"cumulative_funding":  inp.CumulativeFunding,
			"quote_asset":         inp.QuoteAsset,
			"start_time":          inp.StartTime,
		}
		if inp.ExitPrice != "" {
			obj["exit_price"] = inp.ExitPrice
		}
		if inp.EndTime != 0 {
			obj["end_time"] = inp.EndTime
		}
		if inp.OrderID != "" {
			obj["order_id"] = inp.OrderID
		}
		objects[i] = obj
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertPositions struct {
			Returning []*Position `json:"returning"`
		} `json:"insert_positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add positions: %w", err)
	}

	return resp.InsertPositions.Returning, nil
}

// AddPositionEvents batch-inserts position events.
func (c *Client) AddPositionEvents(ctx context.Context, inputs []*PositionEventInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	query := `
		mutation AddPositionEvents($objects: [position_events_insert_input!]!) {
			insert_position_events(objects: $objects) {
				affected_rows
			}
		}
	`

	objects := make([]map[string]interface{}, len(inputs))
	for i, inp := range inputs {
		obj := map[string]interface{}{
			"position_id": inp.PositionID.String(),
			"event_type":  inp.EventType,
			"event_id":    inp.EventID.String(),
			"direction":   inp.Direction,
			"quantity":    inp.Quantity,
			"timestamp":   inp.Timestamp,
		}
		if inp.Price != "" {
			obj["price"] = inp.Price
		}
		objects[i] = obj
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertPositionEvents struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_position_events"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add position events: %w", err)
	}

	return resp.InsertPositionEvents.AffectedRows, nil
}

// GetPositions queries positions with filters.
func (c *Client) GetPositions(ctx context.Context, filter PositionFilter) ([]*Position, error) {
	where := make(map[string]interface{})

	if len(filter.ExchangeAccountIDs) > 0 {
		ids := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			ids[i] = id.String()
		}
		where["exchange_account_id"] = map[string]interface{}{"_in": ids}
	}
	if filter.Status != nil {
		where["status"] = map[string]interface{}{"_eq": *filter.Status}
	}
	if filter.MarketType != nil {
		where["market_type"] = map[string]interface{}{"_eq": *filter.MarketType}
	}
	if filter.Market != nil {
		where["market"] = map[string]interface{}{"_eq": *filter.Market}
	}

	query := `
		query GetPositions($where: positions_bool_exp!) {
			positions(where: $where, order_by: { start_time: desc }) {
				id
				exchange_account_id
				market
				market_type
				side
				status
				quantity
				entry_price
				exit_price
				quote_asset
				total_fees
				cumulative_funding
				start_time
				end_time
				order_id
				updated_at
				exchange_account {
					id
					exchange_id
					account_identifier
					label
					account_type
					tags
					exchange {
						id
						name
						display_name
					}
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"where": where,
	})

	var resp struct {
		Positions []*Position `json:"positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	return resp.Positions, nil
}

// GetPositionEventsByPositionID returns all position events for a given position,
// ordered by timestamp ascending (chronological trade order).
func (c *Client) GetPositionEventsByPositionID(ctx context.Context, positionID uuid.UUID) ([]*PositionEvent, error) {
	query := `
		query GetPositionEventsByPositionID($position_id: uuid!) {
			position_events(
				where: { position_id: { _eq: $position_id } }
				order_by: { timestamp: asc }
			) {
				id
				position_id
				event_type
				event_id
				direction
				quantity
				price
				timestamp
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"position_id": positionID.String(),
	})

	var resp struct {
		PositionEvents []*PositionEvent `json:"position_events"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get position events: %w", err)
	}

	return resp.PositionEvents, nil
}

// UpdatePositionPnL sets realized_pnl and pnl_denomination on a single position.
func (c *Client) UpdatePositionPnL(ctx context.Context, positionID uuid.UUID, realizedPnl float64, denomination string) error {
	query := `
		mutation UpdatePositionPnL($id: uuid!, $realized_pnl: numeric!, $pnl_denomination: String!) {
			update_positions_by_pk(
				pk_columns: { id: $id }
				_set: {
					realized_pnl: $realized_pnl
					pnl_denomination: $pnl_denomination
					updated_at: "now()"
				}
			) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":               positionID.String(),
		"realized_pnl":     realizedPnl,
		"pnl_denomination": denomination,
	})

	var resp struct {
		UpdatePositionsByPk *struct {
			ID string `json:"id"`
		} `json:"update_positions_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update position PnL: %w", err)
	}

	if resp.UpdatePositionsByPk == nil {
		return notFoundError("position", positionID.String())
	}

	return nil
}

// GetPositionsWithNullPnL returns closed positions that don't yet have realized_pnl computed.
func (c *Client) GetPositionsWithNullPnL(ctx context.Context, since time.Time, limit int) ([]*Position, error) {
	query := `
		query GetPositionsWithNullPnL($since: timestamptz!, $limit: Int!) {
			positions(
				where: {
					status: { _eq: "closed" }
					realized_pnl: { _is_null: true }
					end_time: { _gte: $since }
				}
				order_by: { end_time: asc }
				limit: $limit
			) {
				id
				exchange_account_id
				market
				market_type
				side
				status
				quantity
				entry_price
				exit_price
				quote_asset
				total_fees
				cumulative_funding
				start_time
				end_time
				order_id
				updated_at
				exchange_account {
					id
					exchange_id
					account_identifier
					label
					account_type
					tags
					exchange {
						id
						name
						display_name
					}
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"since": since.Format(time.RFC3339),
		"limit": limit,
	})

	var resp struct {
		Positions []*Position `json:"positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get positions with null PnL: %w", err)
	}

	return resp.Positions, nil
}
