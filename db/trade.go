package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Trade represents a trade model (aliased from models package)
type Trade = models.Trade

// TradeInput represents trade input for mutations (aliased from models package)
type TradeInput = models.TradeInput

// TradeFilter represents filtering options for listing trades
type TradeFilter = models.TradeFilter

// GetTrade retrieves a single trade by ID
func (c *Client) GetTrade(ctx context.Context, id string) (*Trade, error) {
	query := `
		query GetTrade($id: uuid!) {
			trades_by_pk(id: $id) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		TradesByPk *Trade `json:"trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	if resp.TradesByPk == nil {
		return nil, fmt.Errorf("trade not found: %s", id)
	}

	return resp.TradesByPk, nil
}

// ListTrades retrieves trades with optional filtering
func (c *Client) ListTrades(ctx context.Context, filter TradeFilter) ([]*Trade, error) {
	var query string
	var vars map[string]interface{}

	// Build query based on whether we're filtering by account IDs
	if len(filter.ExchangeAccountIDs) > 0 {
		// Filter by specific account IDs
		query = `
			query ListTrades($exchange_account_ids: [uuid!]!) {
				trades(
					where: {
						exchange_account_id: {
							_in: $exchange_account_ids
						}
					}
					order_by: { timestamp: desc }
				) {
					id
					base_asset
					quote_asset
					side
					price
					quantity
					timestamp
					fee
					order_id
					trade_id
					exchange_account_id
					created_at
				}
			}
		`
		// Convert UUIDs to strings for GraphQL
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars = map[string]interface{}{
			"exchange_account_ids": accountIDs,
		}
	} else {
		// No filter - get all trades
		query = `
			query ListTrades {
				trades(
					order_by: { timestamp: desc }
				) {
					id
					base_asset
					quote_asset
					side
					price
					quantity
					timestamp
					fee
					order_id
					trade_id
					exchange_account_id
					created_at
				}
			}
		`
		vars = map[string]interface{}{}
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		Trades []*Trade `json:"trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list trades: %w", err)
	}

	return resp.Trades, nil
}

// CreateTrade creates a new trade
func (c *Client) CreateTrade(ctx context.Context, input *TradeInput) (*Trade, error) {
	query := `
		mutation CreateTrade(
			$base_asset: String!
			$quote_asset: String!
			$side: String!
			$price: numeric!
			$quantity: numeric!
			$timestamp: timestamptz!
			$fee: numeric!
			$order_id: String!
			$trade_id: String!
			$exchange_account_id: uuid!
		) {
			insert_trades_one(object: {
				base_asset: $base_asset
				quote_asset: $quote_asset
				side: $side
				price: $price
				quantity: $quantity
				timestamp: $timestamp
				fee: $fee
				order_id: $order_id
				trade_id: $trade_id
				exchange_account_id: $exchange_account_id
			}) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	vars := map[string]interface{}{
		"base_asset":         input.BaseAsset,
		"quote_asset":        input.QuoteAsset,
		"side":               input.Side,
		"price":              input.Price,
		"quantity":           input.Quantity,
		"timestamp":          input.Timestamp.Format(time.RFC3339),
		"fee":                input.Fee,
		"order_id":           input.OrderID,
		"trade_id":           input.TradeID,
		"exchange_account_id": input.ExchangeAccountID.String(),
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertTradesOne *Trade `json:"insert_trades_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create trade: %w", err)
	}

	if resp.InsertTradesOne == nil {
		return nil, fmt.Errorf("failed to create trade: no data returned")
	}

	return resp.InsertTradesOne, nil
}

// UpdateTrade updates an existing trade
func (c *Client) UpdateTrade(ctx context.Context, id string, input *TradeInput) (*Trade, error) {
	query := `
		mutation UpdateTrade(
			$id: uuid!
			$base_asset: String!
			$quote_asset: String!
			$side: String!
			$price: numeric!
			$quantity: numeric!
			$timestamp: timestamptz!
			$fee: numeric!
			$order_id: String!
			$trade_id: String!
			$exchange_account_id: uuid!
		) {
			update_trades_by_pk(
				pk_columns: { id: $id }
				_set: {
					base_asset: $base_asset
					quote_asset: $quote_asset
					side: $side
					price: $price
					quantity: $quantity
					timestamp: $timestamp
					fee: $fee
					order_id: $order_id
					trade_id: $trade_id
					exchange_account_id: $exchange_account_id
				}
			) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	vars := map[string]interface{}{
		"id":                 id,
		"base_asset":         input.BaseAsset,
		"quote_asset":        input.QuoteAsset,
		"side":               input.Side,
		"price":              input.Price,
		"quantity":           input.Quantity,
		"timestamp":          input.Timestamp.Format(time.RFC3339),
		"fee":                input.Fee,
		"order_id":           input.OrderID,
		"trade_id":           input.TradeID,
		"exchange_account_id": input.ExchangeAccountID.String(),
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		UpdateTradesByPk *Trade `json:"update_trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to update trade: %w", err)
	}

	if resp.UpdateTradesByPk == nil {
		return nil, fmt.Errorf("trade not found: %s", id)
	}

	return resp.UpdateTradesByPk, nil
}

// DeleteTrade deletes a trade by ID
func (c *Client) DeleteTrade(ctx context.Context, id string) error {
	query := `
		mutation DeleteTrade($id: uuid!) {
			delete_trades_by_pk(id: $id) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		DeleteTradesByPk struct {
			ID string `json:"id"`
		} `json:"delete_trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete trade: %w", err)
	}

	if resp.DeleteTradesByPk.ID == "" {
		return fmt.Errorf("trade not found: %s", id)
	}

	return nil
}

// LatestTrade retrieves the latest trade for each specified exchange account
// Returns a map of exchange_account_id -> latest trade
func (c *Client) LatestTrade(ctx context.Context, exchangeAccountIDs []uuid.UUID) (map[uuid.UUID]*Trade, error) {
	if len(exchangeAccountIDs) == 0 {
		return make(map[uuid.UUID]*Trade), nil
	}

	query := `
		query LatestTrade($exchange_account_ids: [uuid!]!) {
			trades(
				where: {
					exchange_account_id: {
						_in: $exchange_account_ids
					}
				}
				order_by: { timestamp: desc }
			) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	// Convert UUIDs to strings for GraphQL
	accountIDs := make([]string, len(exchangeAccountIDs))
	for i, id := range exchangeAccountIDs {
		accountIDs[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_ids": accountIDs,
	})

	var resp struct {
		Trades []*Trade `json:"trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest trades: %w", err)
	}

	// Build map: for each account, keep only the first (latest) trade
	result := make(map[uuid.UUID]*Trade)
	seen := make(map[uuid.UUID]bool)

	for _, trade := range resp.Trades {
		if !seen[trade.ExchangeAccountID] {
			result[trade.ExchangeAccountID] = trade
			seen[trade.ExchangeAccountID] = true
		}
	}

	return result, nil
}

// Same methods for ClientWithGraphQL (for testing)

func (c *ClientWithGraphQL) GetTrade(ctx context.Context, id string) (*Trade, error) {
	query := `
		query GetTrade($id: uuid!) {
			trades_by_pk(id: $id) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		TradesByPk *Trade `json:"trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get trade: %w", err)
	}

	if resp.TradesByPk == nil {
		return nil, fmt.Errorf("trade not found: %s", id)
	}

	return resp.TradesByPk, nil
}

func (c *ClientWithGraphQL) ListTrades(ctx context.Context, filter TradeFilter) ([]*Trade, error) {
	var query string
	var vars map[string]interface{}

	if len(filter.ExchangeAccountIDs) > 0 {
		query = `
			query ListTrades($exchange_account_ids: [uuid!]!) {
				trades(
					where: {
						exchange_account_id: {
							_in: $exchange_account_ids
						}
					}
					order_by: { timestamp: desc }
				) {
					id
					base_asset
					quote_asset
					side
					price
					quantity
					timestamp
					fee
					order_id
					trade_id
					exchange_account_id
					created_at
				}
			}
		`
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars = map[string]interface{}{
			"exchange_account_ids": accountIDs,
		}
	} else {
		query = `
			query ListTrades {
				trades(
					order_by: { timestamp: desc }
				) {
					id
					base_asset
					quote_asset
					side
					price
					quantity
					timestamp
					fee
					order_id
					trade_id
					exchange_account_id
					created_at
				}
			}
		`
		vars = map[string]interface{}{}
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		Trades []*Trade `json:"trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list trades: %w", err)
	}

	return resp.Trades, nil
}

func (c *ClientWithGraphQL) CreateTrade(ctx context.Context, input *TradeInput) (*Trade, error) {
	query := `
		mutation CreateTrade(
			$base_asset: String!
			$quote_asset: String!
			$side: String!
			$price: numeric!
			$quantity: numeric!
			$timestamp: timestamptz!
			$fee: numeric!
			$order_id: String!
			$trade_id: String!
			$exchange_account_id: uuid!
		) {
			insert_trades_one(object: {
				base_asset: $base_asset
				quote_asset: $quote_asset
				side: $side
				price: $price
				quantity: $quantity
				timestamp: $timestamp
				fee: $fee
				order_id: $order_id
				trade_id: $trade_id
				exchange_account_id: $exchange_account_id
			}) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	vars := map[string]interface{}{
		"base_asset":         input.BaseAsset,
		"quote_asset":        input.QuoteAsset,
		"side":               input.Side,
		"price":              input.Price,
		"quantity":           input.Quantity,
		"timestamp":          input.Timestamp.Format(time.RFC3339),
		"fee":                input.Fee,
		"order_id":           input.OrderID,
		"trade_id":           input.TradeID,
		"exchange_account_id": input.ExchangeAccountID.String(),
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertTradesOne *Trade `json:"insert_trades_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create trade: %w", err)
	}

	if resp.InsertTradesOne == nil {
		return nil, fmt.Errorf("failed to create trade: no data returned")
	}

	return resp.InsertTradesOne, nil
}

func (c *ClientWithGraphQL) UpdateTrade(ctx context.Context, id string, input *TradeInput) (*Trade, error) {
	query := `
		mutation UpdateTrade(
			$id: uuid!
			$base_asset: String!
			$quote_asset: String!
			$side: String!
			$price: numeric!
			$quantity: numeric!
			$timestamp: timestamptz!
			$fee: numeric!
			$order_id: String!
			$trade_id: String!
			$exchange_account_id: uuid!
		) {
			update_trades_by_pk(
				pk_columns: { id: $id }
				_set: {
					base_asset: $base_asset
					quote_asset: $quote_asset
					side: $side
					price: $price
					quantity: $quantity
					timestamp: $timestamp
					fee: $fee
					order_id: $order_id
					trade_id: $trade_id
					exchange_account_id: $exchange_account_id
				}
			) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	vars := map[string]interface{}{
		"id":                 id,
		"base_asset":         input.BaseAsset,
		"quote_asset":        input.QuoteAsset,
		"side":               input.Side,
		"price":              input.Price,
		"quantity":           input.Quantity,
		"timestamp":          input.Timestamp.Format(time.RFC3339),
		"fee":                input.Fee,
		"order_id":           input.OrderID,
		"trade_id":           input.TradeID,
		"exchange_account_id": input.ExchangeAccountID.String(),
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		UpdateTradesByPk *Trade `json:"update_trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to update trade: %w", err)
	}

	if resp.UpdateTradesByPk == nil {
		return nil, fmt.Errorf("trade not found: %s", id)
	}

	return resp.UpdateTradesByPk, nil
}

func (c *ClientWithGraphQL) DeleteTrade(ctx context.Context, id string) error {
	query := `
		mutation DeleteTrade($id: uuid!) {
			delete_trades_by_pk(id: $id) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		DeleteTradesByPk struct {
			ID string `json:"id"`
		} `json:"delete_trades_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete trade: %w", err)
	}

	if resp.DeleteTradesByPk.ID == "" {
		return fmt.Errorf("trade not found: %s", id)
	}

	return nil
}

func (c *ClientWithGraphQL) LatestTrade(ctx context.Context, exchangeAccountIDs []uuid.UUID) (map[uuid.UUID]*Trade, error) {
	if len(exchangeAccountIDs) == 0 {
		return make(map[uuid.UUID]*Trade), nil
	}

	query := `
		query LatestTrade($exchange_account_ids: [uuid!]!) {
			trades(
				where: {
					exchange_account_id: {
						_in: $exchange_account_ids
					}
				}
				order_by: { timestamp: desc }
			) {
				id
				base_asset
				quote_asset
				side
				price
				quantity
				timestamp
				fee
				order_id
				trade_id
				exchange_account_id
				created_at
			}
		}
	`

	accountIDs := make([]string, len(exchangeAccountIDs))
	for i, id := range exchangeAccountIDs {
		accountIDs[i] = id.String()
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_ids": accountIDs,
	})

	var resp struct {
		Trades []*Trade `json:"trades"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest trades: %w", err)
	}

	result := make(map[uuid.UUID]*Trade)
	seen := make(map[uuid.UUID]bool)

	for _, trade := range resp.Trades {
		if !seen[trade.ExchangeAccountID] {
			result[trade.ExchangeAccountID] = trade
			seen[trade.ExchangeAccountID] = true
		}
	}

	return result, nil
}
