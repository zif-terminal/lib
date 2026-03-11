package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Transfer aliased from models
type Transfer = models.Transfer

// TransferFilter aliased from models
type TransferFilter = models.TransferFilter

// TransferInput aliased from models
type TransferInput = models.TransferInput

// ListTransfers queries the transfers table with optional account ID filter.
func (c *Client) ListTransfers(ctx context.Context, filter TransferFilter) ([]*Transfer, error) {
	where := map[string]interface{}{}
	if len(filter.ExchangeAccountIDs) > 0 {
		ids := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			ids[i] = id.String()
		}
		where["exchange_account_id"] = map[string]interface{}{
			"_in": ids,
		}
	}

	query := `
		query ListTransfers($where: transfers_bool_exp!) {
			transfers(where: $where, order_by: { timestamp: asc }) {
				id
				exchange_account_id
				type
				asset
				amount
				timestamp
				cost_basis
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"where": where,
	})

	var resp struct {
		Transfers []*Transfer `json:"transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list transfers: %w", err)
	}

	return resp.Transfers, nil
}

// AddTransfers inserts transfer records into the transfers table.
// Uses ON CONFLICT DO NOTHING to avoid duplicates.
func (c *Client) AddTransfers(ctx context.Context, inputs []*TransferInput) ([]*Transfer, error) {
	if len(inputs) == 0 {
		return []*Transfer{}, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"type":                input.Type,
			"asset":               input.Asset,
			"amount":              input.Amount,
			"timestamp":           input.Timestamp.UnixMilli(),
		}
		if input.CostBasis != "" {
			obj["cost_basis"] = input.CostBasis
		}
		objects[i] = obj
	}

	query := `
		mutation AddTransfers($objects: [transfers_insert_input!]!) {
			insert_transfers(objects: $objects, on_conflict: {constraint: transfers_pkey, update_columns: []}) {
				returning {
					id
					exchange_account_id
					type
					asset
					amount
					timestamp
					cost_basis
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertTransfers struct {
			Returning []*Transfer `json:"returning"`
		} `json:"insert_transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add transfers: %w", err)
	}

	return resp.InsertTransfers.Returning, nil
}

// DeleteTransfersByAccountAndType deletes transfer records matching account ID and type.
func (c *Client) DeleteTransfersByAccountAndType(ctx context.Context, accountID uuid.UUID, transferType string) (int, error) {
	query := `
		mutation DeleteTransfers($exchange_account_id: uuid!, $type: String!) {
			delete_transfers(where: {
				exchange_account_id: { _eq: $exchange_account_id }
				type: { _eq: $type }
			}) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
		"type":                transferType,
	})

	var resp struct {
		DeleteTransfers struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete transfers: %w", err)
	}

	return resp.DeleteTransfers.AffectedRows, nil
}
