package db

import (
	"context"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// Exchange represents an exchange model (aliased from models package)
type Exchange = models.Exchange

// ExchangeInput represents exchange input for mutations (aliased from models package)
type ExchangeInput = models.ExchangeInput

// GetExchange retrieves a single exchange by ID
func (c *Client) GetExchange(ctx context.Context, id string) (*Exchange, error) {
	query := `
		query GetExchange($id: uuid!) {
			exchanges_by_pk(id: $id) {
				id
				name
				display_name
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id,
	})

	var resp struct {
		ExchangesByPk *Exchange `json:"exchanges_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get exchange: %w", err)
	}

	if resp.ExchangesByPk == nil {
		return nil, fmt.Errorf("exchange not found: %s", id)
	}

	return resp.ExchangesByPk, nil
}

// ListExchanges retrieves all exchanges
func (c *Client) ListExchanges(ctx context.Context) ([]*Exchange, error) {
	query := `
		query ListExchanges {
			exchanges {
				id
				name
				display_name
			}
		}
	`

	req := c.graphqlRequest(query)

	var resp struct {
		Exchanges []*Exchange `json:"exchanges"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list exchanges: %w", err)
	}

	return resp.Exchanges, nil
}

// CreateExchange creates a new exchange
func (c *Client) CreateExchange(ctx context.Context, input *ExchangeInput) (*Exchange, error) {
	query := `
		mutation CreateExchange($name: String!, $display_name: String!) {
			insert_exchanges_one(object: {
				name: $name
				display_name: $display_name
			}) {
				id
				name
				display_name
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"name":         input.Name,
		"display_name": input.DisplayName,
	})

	var resp struct {
		InsertExchangesOne *Exchange `json:"insert_exchanges_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to create exchange: %w", err)
	}

	if resp.InsertExchangesOne == nil {
		return nil, fmt.Errorf("failed to create exchange: no data returned")
	}

	return resp.InsertExchangesOne, nil
}

// UpdateExchange updates an existing exchange
func (c *Client) UpdateExchange(ctx context.Context, id string, input *ExchangeInput) (*Exchange, error) {
	query := `
		mutation UpdateExchange($id: uuid!, $name: String!, $display_name: String!) {
			update_exchanges_by_pk(pk_columns: {id: $id}, _set: {
				name: $name
				display_name: $display_name
			}) {
				id
				name
				display_name
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":           id,
		"name":         input.Name,
		"display_name": input.DisplayName,
	})

	var resp struct {
		UpdateExchangesByPk *Exchange `json:"update_exchanges_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to update exchange: %w", err)
	}

	if resp.UpdateExchangesByPk == nil {
		return nil, fmt.Errorf("exchange not found: %s", id)
	}

	return resp.UpdateExchangesByPk, nil
}
