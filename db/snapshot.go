package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// BalanceSnapshot aliased from models (used by activity_processor for reconciliation)
type BalanceSnapshot = models.BalanceSnapshot

// SpotBalanceSnapshot aliased from models (DB record type)
type SpotBalanceSnapshot = models.SpotBalanceSnapshot

// SpotBalanceSnapshotInput aliased from models
type SpotBalanceSnapshotInput = models.SpotBalanceSnapshotInput

// InterestPayment aliased from models
type InterestPayment = models.InterestPayment

// InterestPaymentInput aliased from models
type InterestPaymentInput = models.InterestPaymentInput

// GetLatestBalanceSnapshots returns the most recent balance snapshot per asset
// for the given account. It queries the spot_balance_snapshots table using
// distinct_on to get only the latest entry per asset.
// Returns float64-based BalanceSnapshot (used by activity_processor).
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

// ---------------------------------------------------------------------------
// Snapshot write methods (used by account_sync via SnapshotDBClient)
// ---------------------------------------------------------------------------

// AddSpotBalanceSnapshots inserts spot balance snapshot records.
func (c *Client) AddSpotBalanceSnapshots(ctx context.Context, inputs []*SpotBalanceSnapshotInput) ([]*SpotBalanceSnapshot, error) {
	if len(inputs) == 0 {
		return []*SpotBalanceSnapshot{}, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"balance":             input.Balance,
			"oracle_price":        input.OraclePrice,
			"usd_value":           input.USDValue,
			"timestamp":           input.Timestamp.UnixMilli(),
		}
	}

	query := `
		mutation AddSpotBalanceSnapshots($objects: [spot_balance_snapshots_insert_input!]!) {
			insert_spot_balance_snapshots(objects: $objects) {
				returning {
					id
					exchange_account_id
					asset
					balance
					oracle_price
					usd_value
					timestamp
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		Insert struct {
			Returning []*SpotBalanceSnapshot `json:"returning"`
		} `json:"insert_spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add spot balance snapshots: %w", err)
	}

	return resp.Insert.Returning, nil
}

// GetLatestSpotBalanceSnapshot returns the most recent snapshot for a specific asset.
func (c *Client) GetLatestSpotBalanceSnapshot(ctx context.Context, accountID uuid.UUID, asset string) (*SpotBalanceSnapshot, error) {
	query := `
		query GetLatestSpotBalanceSnapshot($account_id: uuid!, $asset: String!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $account_id }
					asset: { _eq: $asset }
				}
				order_by: { timestamp: desc }
				limit: 1
			) {
				id
				exchange_account_id
				asset
				balance
				oracle_price
				usd_value
				timestamp
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"asset":      asset,
	})

	var resp struct {
		Snapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest spot balance snapshot: %w", err)
	}

	if len(resp.Snapshots) == 0 {
		return nil, nil
	}

	return resp.Snapshots[0], nil
}

// GetSpotBalanceSnapshotsBefore returns the most recent snapshot before a given timestamp.
func (c *Client) GetSpotBalanceSnapshotsBefore(ctx context.Context, accountID uuid.UUID, asset string, beforeMs int64) (*SpotBalanceSnapshot, error) {
	query := `
		query GetSpotBalanceSnapshotsBefore($account_id: uuid!, $asset: String!, $before_ms: bigint!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $account_id }
					asset: { _eq: $asset }
					timestamp: { _lt: $before_ms }
				}
				order_by: { timestamp: desc }
				limit: 1
			) {
				id
				exchange_account_id
				asset
				balance
				oracle_price
				usd_value
				timestamp
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"asset":      asset,
		"before_ms":  beforeMs,
	})

	var resp struct {
		Snapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get spot balance snapshots before: %w", err)
	}

	if len(resp.Snapshots) == 0 {
		return nil, nil
	}

	return resp.Snapshots[0], nil
}

// PruneOldSpotBalanceSnapshots deletes snapshots older than the given timestamp.
func (c *Client) PruneOldSpotBalanceSnapshots(ctx context.Context, beforeMs int64) (int, error) {
	query := `
		mutation PruneOldSpotBalanceSnapshots($before_ms: bigint!) {
			delete_spot_balance_snapshots(
				where: { timestamp: { _lt: $before_ms } }
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"before_ms": beforeMs,
	})

	var resp struct {
		Delete struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to prune old spot balance snapshots: %w", err)
	}

	return resp.Delete.AffectedRows, nil
}

// ---------------------------------------------------------------------------
// Interest payment methods
// ---------------------------------------------------------------------------

// AddInterestPayments inserts interest payment records, skipping duplicates
// via the unique constraint on (exchange_account_id, asset, snapshot_from, snapshot_to).
func (c *Client) AddInterestPayments(ctx context.Context, inputs []*InterestPaymentInput) ([]*InterestPayment, error) {
	if len(inputs) == 0 {
		return []*InterestPayment{}, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"amount":              input.Amount,
			"oracle_price":        input.OraclePrice,
			"usd_value":           input.USDValue,
			"timestamp":           input.Timestamp.UnixMilli(),
			"snapshot_from":       input.SnapshotFrom,
			"snapshot_to":         input.SnapshotTo,
			"is_approximate":      input.IsApproximate,
		}
	}

	query := `
		mutation AddInterestPayments($objects: [interest_payments_insert_input!]!) {
			insert_interest_payments(
				objects: $objects
				on_conflict: {
					constraint: interest_payments_unique_snapshot
					update_columns: []
				}
			) {
				returning {
					id
					exchange_account_id
					asset
					amount
					oracle_price
					usd_value
					timestamp
					snapshot_from
					snapshot_to
					is_approximate
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"objects": objects,
	})

	var resp struct {
		Insert struct {
			Returning []*InterestPayment `json:"returning"`
		} `json:"insert_interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add interest payments: %w", err)
	}

	return resp.Insert.Returning, nil
}

// DeleteInterestPaymentsForAccount deletes all interest payments for an account.
func (c *Client) DeleteInterestPaymentsForAccount(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		mutation DeleteInterestPaymentsForAccount($account_id: uuid!) {
			delete_interest_payments(
				where: { exchange_account_id: { _eq: $account_id } }
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		Delete struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete interest payments: %w", err)
	}

	return resp.Delete.AffectedRows, nil
}

// SumInterestForAccount returns the sum of interest amounts per asset for an account
// in the given time range [fromMs, toMs].
func (c *Client) SumInterestForAccount(ctx context.Context, accountID uuid.UUID, fromMs, toMs int64) (map[string]string, error) {
	query := `
		query SumInterestForAccount($account_id: uuid!, $from_ms: bigint!, $to_ms: bigint!) {
			interest_payments(
				where: {
					exchange_account_id: { _eq: $account_id }
					timestamp: { _gte: $from_ms, _lte: $to_ms }
				}
			) {
				asset
				amount
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"from_ms":    fromMs,
		"to_ms":      toMs,
	})

	var resp struct {
		Payments []struct {
			Asset  string `json:"asset"`
			Amount string `json:"amount"`
		} `json:"interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to sum interest for account: %w", err)
	}

	// Aggregate amounts by asset
	sums := make(map[string]string)
	for _, p := range resp.Payments {
		if existing, ok := sums[p.Asset]; ok {
			existingF := parseFloat64(existing)
			amountF := parseFloat64(p.Amount)
			sums[p.Asset] = strconv.FormatFloat(existingF+amountF, 'f', 18, 64)
		} else {
			sums[p.Asset] = p.Amount
		}
	}

	return sums, nil
}

// ---------------------------------------------------------------------------
// Funding range sum
// ---------------------------------------------------------------------------

// SumFundingInRange returns the sum of all funding payment amounts for an account
// in the half-open interval (fromMs, toMs].
func (c *Client) SumFundingInRange(ctx context.Context, accountID uuid.UUID, fromMs, toMs int64) (string, error) {
	query := `
		query SumFundingInRange($account_id: uuid!, $from_ms: bigint!, $to_ms: bigint!) {
			funding_payments_aggregate(
				where: {
					exchange_account_id: { _eq: $account_id }
					timestamp: { _gt: $from_ms, _lte: $to_ms }
				}
			) {
				aggregate {
					sum {
						amount
					}
				}
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"from_ms":    fromMs,
		"to_ms":      toMs,
	})

	var resp struct {
		Aggregate struct {
			Aggregate struct {
				Sum struct {
					Amount *string `json:"amount"`
				} `json:"sum"`
			} `json:"aggregate"`
		} `json:"funding_payments_aggregate"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return "0", fmt.Errorf("failed to sum funding in range: %w", err)
	}

	if resp.Aggregate.Aggregate.Sum.Amount == nil {
		return "0", nil
	}

	return *resp.Aggregate.Aggregate.Sum.Amount, nil
}

// ---------------------------------------------------------------------------
// Account metadata
// ---------------------------------------------------------------------------

// UpdateAccountTypeMetadata updates the account_type_metadata JSONB field on an exchange account.
func (c *Client) UpdateAccountTypeMetadata(ctx context.Context, accountID uuid.UUID, metadata json.RawMessage) error {
	query := `
		mutation UpdateAccountTypeMetadata($id: uuid!, $metadata: jsonb!) {
			update_exchange_accounts_by_pk(
				pk_columns: { id: $id }
				_set: { account_type_metadata: $metadata }
			) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":       accountID.String(),
		"metadata": string(metadata),
	})

	var resp struct {
		Update *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update account type metadata: %w", err)
	}

	return nil
}
