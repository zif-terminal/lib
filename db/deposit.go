package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Deposit represents a deposit model (aliased from models package)
type Deposit = models.Deposit

// DepositInput represents deposit input for mutations (aliased from models package)
type DepositInput = models.DepositInput

// DepositFilter represents deposit filter (aliased from models package)
type DepositFilter = models.DepositFilter

// GetLatestDeposit retrieves the latest deposit for an exchange account
func (c *Client) GetLatestDeposit(ctx context.Context, exchangeAccountID uuid.UUID) (*Deposit, error) {
	query := `
		query GetLatestDeposit($exchange_account_id: uuid!) {
			deposits(
				where: {
					exchange_account_id: {
						_eq: $exchange_account_id
					}
				}
				order_by: { timestamp: desc }
				limit: 1
			) {
				id
				exchange_account_id
				asset
				direction
				amount
				user_cost_basis
				timestamp
				deposit_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
	})

	var resp struct {
		Deposits []*Deposit `json:"deposits"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest deposit: %w", err)
	}

	if len(resp.Deposits) == 0 {
		return nil, nil // No deposit found - return nil, no error
	}

	return resp.Deposits[0], nil
}

// AddDeposits adds one or many deposits
// Uses batch insert for all cases (even single deposit)
func (c *Client) AddDeposits(ctx context.Context, inputs []*DepositInput) ([]*Deposit, error) {
	if len(inputs) == 0 {
		return []*Deposit{}, nil
	}

	// Convert inputs to GraphQL format
	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"direction":           input.Direction,
			"amount":              input.Amount,
			"user_cost_basis":     input.UserCostBasis,
			"timestamp":           input.Timestamp.UnixMilli(),
			"deposit_id":          input.DepositID,
		}
	}

	// Always use batch insert, even for single deposit
	query := `
		mutation AddDeposits($objects: [deposits_insert_input!]!) {
			insert_deposits(objects: $objects) {
				returning {
					id
					exchange_account_id
					asset
					direction
					amount
					user_cost_basis
					timestamp
					deposit_id
				}
			}
		}
	`

	vars := map[string]interface{}{
		"objects": objects,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertDeposits struct {
			Returning []*Deposit `json:"returning"`
		} `json:"insert_deposits"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add deposits: %w", err)
	}

	return resp.InsertDeposits.Returning, nil
}

// UpdateDepositCostBasis updates the user_cost_basis for a specific deposit
func (c *Client) UpdateDepositCostBasis(ctx context.Context, depositID uuid.UUID, userCostBasis string) (*Deposit, error) {
	query := `
		mutation UpdateDepositCostBasis($id: uuid!, $user_cost_basis: numeric!) {
			update_deposits_by_pk(
				pk_columns: { id: $id }
				_set: { user_cost_basis: $user_cost_basis }
			) {
				id
				exchange_account_id
				asset
				direction
				amount
				user_cost_basis
				timestamp
				deposit_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":              depositID.String(),
		"user_cost_basis": userCostBasis,
	})

	var resp struct {
		UpdateDepositsByPK *Deposit `json:"update_deposits_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to update deposit cost basis: %w", err)
	}

	return resp.UpdateDepositsByPK, nil
}

// ListDeposits retrieves deposits with optional filtering
func (c *Client) ListDeposits(ctx context.Context, filter DepositFilter) ([]*Deposit, error) {
	// Build where clause based on filter
	vars := make(map[string]interface{})

	conditions := []string{}

	if len(filter.ExchangeAccountIDs) > 0 {
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars["account_ids"] = accountIDs
		conditions = append(conditions, "exchange_account_id: { _in: $account_ids }")
	}

	if len(filter.Assets) > 0 {
		vars["assets"] = filter.Assets
		conditions = append(conditions, "asset: { _in: $assets }")
	}

	if len(filter.Directions) > 0 {
		vars["directions"] = filter.Directions
		conditions = append(conditions, "direction: { _in: $directions }")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "where: { " + joinConditions(conditions) + " }"
	}

	// Build variable declarations
	varDeclarations := ""
	if len(filter.ExchangeAccountIDs) > 0 {
		varDeclarations += "$account_ids: [uuid!]"
	}
	if len(filter.Assets) > 0 {
		if varDeclarations != "" {
			varDeclarations += ", "
		}
		varDeclarations += "$assets: [String!]"
	}
	if len(filter.Directions) > 0 {
		if varDeclarations != "" {
			varDeclarations += ", "
		}
		varDeclarations += "$directions: [String!]"
	}

	queryHeader := "query ListDeposits"
	if varDeclarations != "" {
		queryHeader += "(" + varDeclarations + ")"
	}

	query := fmt.Sprintf(`
		%s {
			deposits(
				%s
				order_by: { timestamp: desc }
			) {
				id
				exchange_account_id
				asset
				direction
				amount
				user_cost_basis
				timestamp
				deposit_id
			}
		}
	`, queryHeader, whereClause)

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		Deposits []*Deposit `json:"deposits"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list deposits: %w", err)
	}

	return resp.Deposits, nil
}

// joinConditions joins GraphQL where conditions with commas
func joinConditions(conditions []string) string {
	result := ""
	for i, cond := range conditions {
		if i > 0 {
			result += ", "
		}
		result += cond
	}
	return result
}
