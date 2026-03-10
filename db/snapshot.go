package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// BalanceSnapshot aliased from models
type BalanceSnapshot = models.BalanceSnapshot

// GetLatestBalanceSnapshots returns the most recent balance snapshot per asset
// for the given account. It queries the spot_balance_snapshots table using
// distinct_on to get only the latest entry per asset.
func (c *Client) GetLatestBalanceSnapshots(ctx context.Context, accountID uuid.UUID) ([]*BalanceSnapshot, error) {
	query := `
		query GetLatestBalanceSnapshots($account_id: uuid!) {
			spot_balance_snapshots(
				where: { exchange_account_id: { _eq: $account_id } }
				distinct_on: asset
				order_by: [{ asset: asc }, { timestamp: desc }]
			) {
				asset
				balance
				oracle_price
				usd_value
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		Snapshots []struct {
			Asset       string `json:"asset"`
			Balance     string `json:"balance"`
			OraclePrice string `json:"oracle_price"`
			UsdValue    string `json:"usd_value"`
		} `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest balance snapshots: %w", err)
	}

	balances := make([]*BalanceSnapshot, 0, len(resp.Snapshots))
	for _, s := range resp.Snapshots {
		balances = append(balances, &BalanceSnapshot{
			Asset:       s.Asset,
			Balance:     parseFloat64(s.Balance),
			OraclePrice: parseFloat64(s.OraclePrice),
			UsdValue:    parseFloat64(s.UsdValue),
		})
	}

	return balances, nil
}

// parseFloat64 parses a numeric string to float64, returning 0 on error.
func parseFloat64(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
