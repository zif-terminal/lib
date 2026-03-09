package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Settlement represents a settlement model (aliased from models package)
type Settlement = models.Settlement

// SettlementInput represents settlement input for mutations (aliased from models package)
type SettlementInput = models.SettlementInput

// SettlementFilter represents settlement filter (aliased from models package)
type SettlementFilter = models.SettlementFilter

// AddSettlements inserts settlement records, skipping duplicates via ON CONFLICT.
func (c *Client) AddSettlements(ctx context.Context, inputs []*SettlementInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"amount":              input.Amount,
			"market":              input.Market,
			"timestamp":           input.Timestamp.UnixMilli(),
			"settlement_id":       input.SettlementID,
		}
	}

	query := `
		mutation AddSettlements($objects: [settlements_insert_input!]!) {
			insert_settlements(
				objects: $objects
				on_conflict: {
					constraint: settlements_exchange_account_id_settlement_id_key
					update_columns: []
				}
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		InsertSettlements struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_settlements"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add settlements: %w", err)
	}

	return resp.InsertSettlements.AffectedRows, nil
}

// ListSettlements retrieves settlements for given accounts, ordered by timestamp ascending.
func (c *Client) ListSettlements(ctx context.Context, filter SettlementFilter) ([]*Settlement, error) {
	vars := make(map[string]interface{})
	whereClause := ""

	if len(filter.ExchangeAccountIDs) > 0 {
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars["account_ids"] = accountIDs
		whereClause = "where: { exchange_account_id: { _in: $account_ids } }"
	}

	queryHeader := "query ListSettlements"
	if len(filter.ExchangeAccountIDs) > 0 {
		queryHeader += "($account_ids: [uuid!])"
	}

	query := fmt.Sprintf(`
		%s {
			settlements(
				%s
				order_by: { timestamp: asc }
			) {
				id
				exchange_account_id
				asset
				amount
				market
				timestamp
				settlement_id
			}
		}
	`, queryHeader, whereClause)

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		Settlements []*Settlement `json:"settlements"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list settlements: %w", err)
	}

	return resp.Settlements, nil
}

// GetLatestSettlement retrieves the most recent settlement for an account.
func (c *Client) GetLatestSettlement(ctx context.Context, exchangeAccountID uuid.UUID) (*Settlement, error) {
	query := `
		query GetLatestSettlement($exchange_account_id: uuid!) {
			settlements(
				where: { exchange_account_id: { _eq: $exchange_account_id } }
				order_by: { timestamp: desc }
				limit: 1
			) {
				id
				exchange_account_id
				asset
				amount
				market
				timestamp
				settlement_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
	})

	var resp struct {
		Settlements []*Settlement `json:"settlements"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest settlement: %w", err)
	}

	if len(resp.Settlements) == 0 {
		return nil, nil
	}

	return resp.Settlements[0], nil
}
