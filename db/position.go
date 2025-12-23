package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Position represents a position model (aliased from models package)
type Position = models.Position

// PositionInput represents position input for mutations (aliased from models package)
type PositionInput = models.PositionInput

// PositionTrade represents a position trade link (aliased from models package)
type PositionTrade = models.PositionTrade

// PositionTradeInput represents position trade input for mutations (aliased from models package)
type PositionTradeInput = models.PositionTradeInput

// PositionFilter represents filtering options for listing positions
type PositionFilter = models.PositionFilter

// GetLastProcessedTradeTimestamp gets the timestamp of the last trade processed into positions
// for a given account and asset pair. Returns nil if no positions exist.
func (c *Client) GetLastProcessedTradeTimestamp(
	ctx context.Context,
	exchangeAccountID uuid.UUID,
	baseAsset string,
	quoteAsset string,
) (*time.Time, error) {
	query := `
		query GetLastProcessedTradeTimestamp(
			$exchange_account_id: uuid!
			$base_asset: String!
			$quote_asset: String!
		) {
			position_trades(
				where: {
					position: {
						exchange_account_id: { _eq: $exchange_account_id }
						base_asset: { _eq: $base_asset }
						quote_asset: { _eq: $quote_asset }
					}
				}
				order_by: { trade: { timestamp: desc } }
				limit: 1
			) {
				trade {
					timestamp
				}
			}
		}
	`

	vars := map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
		"base_asset":          baseAsset,
		"quote_asset":         quoteAsset,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		PositionTrades []struct {
			Trade struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"trade"`
		} `json:"position_trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get last processed trade timestamp: %w", err)
	}

	if len(resp.PositionTrades) == 0 {
		return nil, nil // No positions exist
	}

	timestamp := time.Unix(0, resp.PositionTrades[0].Trade.Timestamp*int64(time.Millisecond)).UTC()
	return &timestamp, nil
}

// CreatePosition creates a new closed position record
func (c *Client) CreatePosition(ctx context.Context, input *PositionInput) (*Position, error) {
	query := `
		mutation CreatePosition(
			$exchange_account_id: uuid!
			$base_asset: String!
			$quote_asset: String!
			$side: String!
			$start_time: bigint!
			$end_time: bigint!
			$entry_avg_price: numeric!
			$exit_avg_price: numeric!
			$total_quantity: numeric!
			$total_fees: numeric!
			$realized_pnl: numeric!
		) {
			insert_positions_one(object: {
				exchange_account_id: $exchange_account_id
				base_asset: $base_asset
				quote_asset: $quote_asset
				side: $side
				start_time: $start_time
				end_time: $end_time
				entry_avg_price: $entry_avg_price
				exit_avg_price: $exit_avg_price
				total_quantity: $total_quantity
				total_fees: $total_fees
				realized_pnl: $realized_pnl
			}) {
				id
				exchange_account_id
				base_asset
				quote_asset
				side
				start_time
				end_time
				entry_avg_price
				exit_avg_price
				total_quantity
				total_fees
				realized_pnl
			}
		}
	`

	vars := map[string]interface{}{
		"exchange_account_id": input.ExchangeAccountID.String(),
		"base_asset":          input.BaseAsset,
		"quote_asset":         input.QuoteAsset,
		"side":                input.Side,
		"start_time":          input.StartTime.UnixMilli(),
		"end_time":            input.EndTime.UnixMilli(),
		"entry_avg_price":     input.EntryAvgPrice,
		"exit_avg_price":      input.ExitAvgPrice,
		"total_quantity":      input.TotalQuantity,
		"total_fees":          input.TotalFees,
		"realized_pnl":        input.RealizedPnL,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertPositionsOne *Position `json:"insert_positions_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create position: %w", err)
	}

	if resp.InsertPositionsOne == nil {
		return nil, fmt.Errorf("failed to create position: no data returned")
	}

	return resp.InsertPositionsOne, nil
}

// CreatePositionTrades batch inserts trade allocations for positions
func (c *Client) CreatePositionTrades(ctx context.Context, inputs []*PositionTradeInput) ([]*PositionTrade, error) {
	if len(inputs) == 0 {
		return []*PositionTrade{}, nil
	}

	query := `
		mutation CreatePositionTrades($objects: [position_trades_insert_input!]!) {
			insert_position_trades(objects: $objects) {
				returning {
					position_id
					trade_id
					allocation_percentage
					allocated_quantity
					allocated_fees
				}
			}
		}
	`

	// Convert inputs to GraphQL format
	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"position_id":           input.PositionID.String(),
			"trade_id":              input.TradeID.String(),
			"allocation_percentage": input.AllocationPercentage,
			"allocated_quantity":    input.AllocatedQuantity,
			"allocated_fees":        input.AllocatedFees,
		}
	}

	vars := map[string]interface{}{
		"objects": objects,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertPositionTrades struct {
			Returning []*PositionTrade `json:"returning"`
		} `json:"insert_position_trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create position trades: %w", err)
	}

	return resp.InsertPositionTrades.Returning, nil
}

// GetPositions queries closed positions with various filters
func (c *Client) GetPositions(ctx context.Context, filter PositionFilter) ([]*Position, error) {
	// Build where clause dynamically based on filter
	whereClause := ""
	vars := make(map[string]interface{})

	if len(filter.ExchangeAccountIDs) > 0 {
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars["exchange_account_ids"] = accountIDs
		whereClause += "exchange_account_id: { _in: $exchange_account_ids }\n"
	}

	if filter.BaseAsset != nil {
		vars["base_asset"] = *filter.BaseAsset
		whereClause += "base_asset: { _eq: $base_asset }\n"
	}

	if filter.QuoteAsset != nil {
		vars["quote_asset"] = *filter.QuoteAsset
		whereClause += "quote_asset: { _eq: $quote_asset }\n"
	}

	if filter.Side != nil {
		vars["side"] = *filter.Side
		whereClause += "side: { _eq: $side }\n"
	}

	if filter.StartTimeGte != nil {
		vars["start_time_gte"] = filter.StartTimeGte.UnixMilli()
		whereClause += "start_time: { _gte: $start_time_gte }\n"
	}

	if filter.StartTimeLte != nil {
		vars["start_time_lte"] = filter.StartTimeLte.UnixMilli()
		whereClause += "start_time: { _lte: $start_time_lte }\n"
	}

	if filter.EndTimeGte != nil {
		vars["end_time_gte"] = filter.EndTimeGte.UnixMilli()
		whereClause += "end_time: { _gte: $end_time_gte }\n"
	}

	if filter.EndTimeLte != nil {
		vars["end_time_lte"] = filter.EndTimeLte.UnixMilli()
		whereClause += "end_time: { _lte: $end_time_lte }\n"
	}

	// Build query with dynamic variable declarations
	varDeclarations := ""
	if len(filter.ExchangeAccountIDs) > 0 {
		varDeclarations += "$exchange_account_ids: [uuid!]!, "
	}
	if filter.BaseAsset != nil {
		varDeclarations += "$base_asset: String!, "
	}
	if filter.QuoteAsset != nil {
		varDeclarations += "$quote_asset: String!, "
	}
	if filter.Side != nil {
		varDeclarations += "$side: String!, "
	}
	if filter.StartTimeGte != nil {
		varDeclarations += "$start_time_gte: bigint!, "
	}
	if filter.StartTimeLte != nil {
		varDeclarations += "$start_time_lte: bigint!, "
	}
	if filter.EndTimeGte != nil {
		varDeclarations += "$end_time_gte: bigint!, "
	}
	if filter.EndTimeLte != nil {
		varDeclarations += "$end_time_lte: bigint!, "
	}

	// Remove trailing comma and space
	if len(varDeclarations) > 0 {
		varDeclarations = varDeclarations[:len(varDeclarations)-2]
	}

	var query string
	if whereClause != "" {
		query = fmt.Sprintf(`
			query GetPositions(%s) {
				positions(
					where: {
						%s
					}
					order_by: { end_time: desc }
				) {
					id
					exchange_account_id
					base_asset
					quote_asset
					side
					start_time
					end_time
					entry_avg_price
					exit_avg_price
					total_quantity
					total_fees
					realized_pnl
				}
			}
		`, varDeclarations, whereClause)
	} else {
		query = `
			query GetPositions {
				positions(order_by: { end_time: desc }) {
					id
					exchange_account_id
					base_asset
					quote_asset
					side
					start_time
					end_time
					entry_avg_price
					exit_avg_price
					total_quantity
					total_fees
					realized_pnl
				}
			}
		`
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		Positions []*Position `json:"positions"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	return resp.Positions, nil
}

// GetPositionByID retrieves a single position with all associated trades
func (c *Client) GetPositionByID(ctx context.Context, positionID string) (*Position, []*PositionTrade, error) {
	query := `
		query GetPositionWithTrades($id: uuid!) {
			positions_by_pk(id: $id) {
				id
				exchange_account_id
				base_asset
				quote_asset
				side
				start_time
				end_time
				entry_avg_price
				exit_avg_price
				total_quantity
				total_fees
				realized_pnl
				position_trades {
					position_id
					trade_id
					allocation_percentage
					allocated_quantity
					allocated_fees
				}
			}
		}
	`

	vars := map[string]interface{}{
		"id": positionID,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		PositionsByPk *struct {
			Position
			PositionTrades []*PositionTrade `json:"position_trades"`
		} `json:"positions_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, nil, fmt.Errorf("failed to get position: %w", err)
	}

	if resp.PositionsByPk == nil {
		return nil, nil, fmt.Errorf("position not found: %s", positionID)
	}

	position := &resp.PositionsByPk.Position
	return position, resp.PositionsByPk.PositionTrades, nil
}
