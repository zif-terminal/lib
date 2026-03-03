package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// SpotBalanceSnapshot aliased from models
type SpotBalanceSnapshot = models.SpotBalanceSnapshot

// SpotBalanceSnapshotInput aliased from models
type SpotBalanceSnapshotInput = models.SpotBalanceSnapshotInput

// InterestPayment aliased from models
type InterestPayment = models.InterestPayment

// InterestPaymentInput aliased from models
type InterestPaymentInput = models.InterestPaymentInput

// AddSpotBalanceSnapshots inserts one or many spot balance snapshots.
// Uses batch insert and ignores conflicts (idempotent on same account+asset+timestamp).
func (c *Client) AddSpotBalanceSnapshots(ctx context.Context, inputs []*SpotBalanceSnapshotInput) ([]*SpotBalanceSnapshot, error) {
	if len(inputs) == 0 {
		return []*SpotBalanceSnapshot{}, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"balance":             input.Balance,
			"timestamp":           input.Timestamp.UnixMilli(),
		}
		if input.OraclePrice != "" && input.OraclePrice != "0" {
			obj["oracle_price"] = input.OraclePrice
		}
		if input.USDValue != "" && input.USDValue != "0" {
			obj["usd_value"] = input.USDValue
		}
		objects[i] = obj
	}

	query := `
		mutation AddSpotBalanceSnapshots($objects: [spot_balance_snapshots_insert_input!]!) {
			insert_spot_balance_snapshots(
				objects: $objects
				on_conflict: {
					constraint: spot_balance_snapshots_pkey
					update_columns: []
				}
			) {
				returning {
					id
					exchange_account_id
					asset
					balance
					oracle_price
					usd_value
					timestamp
					created_at
				}
			}
		}
	`

	vars := map[string]interface{}{
		"objects": objects,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertSpotBalanceSnapshots struct {
			Returning []*SpotBalanceSnapshot `json:"returning"`
		} `json:"insert_spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add spot balance snapshots: %w", err)
	}

	return resp.InsertSpotBalanceSnapshots.Returning, nil
}

// GetLatestSpotBalanceSnapshot retrieves the most recent snapshot for a given account+asset.
// Returns nil if no snapshot exists yet (first-time call).
func (c *Client) GetLatestSpotBalanceSnapshot(ctx context.Context, exchangeAccountID uuid.UUID, asset string) (*SpotBalanceSnapshot, error) {
	query := `
		query GetLatestSpotBalanceSnapshot($exchange_account_id: uuid!, $asset: String!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
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
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
		"asset":               asset,
	})

	var resp struct {
		SpotBalanceSnapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest spot balance snapshot: %w", err)
	}

	if len(resp.SpotBalanceSnapshots) == 0 {
		return nil, nil
	}

	return resp.SpotBalanceSnapshots[0], nil
}

// GetSpotBalanceSnapshotsBefore retrieves the most recent snapshot for a given
// account+asset taken before a given timestamp (Unix ms, exclusive).
// Used by the reconciler to find the "from" snapshot for an interval.
func (c *Client) GetSpotBalanceSnapshotsBefore(ctx context.Context, exchangeAccountID uuid.UUID, asset string, beforeMs int64) (*SpotBalanceSnapshot, error) {
	query := `
		query GetSpotBalanceSnapshotsBefore($exchange_account_id: uuid!, $asset: String!, $before_ts: bigint!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
					asset: { _eq: $asset }
					timestamp: { _lt: $before_ts }
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
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
		"asset":               asset,
		"before_ts":           beforeMs,
	})

	var resp struct {
		SpotBalanceSnapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get snapshot before timestamp: %w", err)
	}

	if len(resp.SpotBalanceSnapshots) == 0 {
		return nil, nil
	}

	return resp.SpotBalanceSnapshots[0], nil
}

// AddInterestPayments inserts derived interest payment records.
// Uses ON CONFLICT DO NOTHING to avoid duplicates (same account+asset+snapshot interval).
func (c *Client) AddInterestPayments(ctx context.Context, inputs []*InterestPaymentInput) ([]*InterestPayment, error) {
	if len(inputs) == 0 {
		return []*InterestPayment{}, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"amount":              input.Amount,
			"timestamp":           input.Timestamp.UnixMilli(),
			"snapshot_from":       input.SnapshotFrom,
			"snapshot_to":         input.SnapshotTo,
			"is_approximate":      input.IsApproximate,
		}
		if input.OraclePrice != "" && input.OraclePrice != "0" {
			obj["oracle_price"] = input.OraclePrice
		}
		if input.USDValue != "" && input.USDValue != "0" {
			obj["usd_value"] = input.USDValue
		}
		objects[i] = obj
	}

	// ON CONFLICT DO NOTHING: idempotent — re-running reconciliation won't create duplicates
	query := `
		mutation AddInterestPayments($objects: [interest_payments_insert_input!]!) {
			insert_interest_payments(
				objects: $objects
				on_conflict: {
					constraint: idx_interest_unique
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
					created_at
				}
			}
		}
	`

	vars := map[string]interface{}{
		"objects": objects,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertInterestPayments struct {
			Returning []*InterestPayment `json:"returning"`
		} `json:"insert_interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to add interest payments: %w", err)
	}

	return resp.InsertInterestPayments.Returning, nil
}

// SumInterestForAccount sums interest payments per asset for an account within a time range.
// Returns a map of asset -> net interest amount (as signed decimal string).
// startTimeMs and endTimeMs are Unix milliseconds; 0 means unbounded.
func (c *Client) SumInterestForAccount(ctx context.Context, accountID uuid.UUID, startTimeMs, endTimeMs int64) (map[string]string, error) {
	// Build time-range filter
	timeFilter := ""
	vars := map[string]interface{}{
		"exchange_account_id": accountID.String(),
	}

	if startTimeMs > 0 && endTimeMs > 0 {
		timeFilter = "timestamp: { _gte: $start_time, _lte: $end_time }"
		vars["start_time"] = startTimeMs
		vars["end_time"] = endTimeMs
	} else if startTimeMs > 0 {
		timeFilter = "timestamp: { _gte: $start_time }"
		vars["start_time"] = startTimeMs
	} else if endTimeMs > 0 {
		timeFilter = "timestamp: { _lte: $end_time }"
		vars["end_time"] = endTimeMs
	}

	whereClause := "exchange_account_id: { _eq: $exchange_account_id }"
	if timeFilter != "" {
		whereClause += ", " + timeFilter
	}

	// Build variable declarations
	varDecls := "$exchange_account_id: uuid!"
	if startTimeMs > 0 {
		varDecls += ", $start_time: bigint!"
	}
	if endTimeMs > 0 {
		varDecls += ", $end_time: bigint!"
	}

	// Query all interest payment rows (not aggregate) to get per-asset breakdown
	query := fmt.Sprintf(`
		query SumInterestForAccount(%s) {
			interest_payments(
				where: { %s }
			) {
				asset
				amount
			}
		}
	`, varDecls, whereClause)

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InterestPayments []struct {
			Asset  string      `json:"asset"`
			Amount interface{} `json:"amount"`
		} `json:"interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to sum interest for account: %w", err)
	}

	// Aggregate by asset client-side (avoids complex Hasura groupBy)
	sums := make(map[string]float64)
	for _, row := range resp.InterestPayments {
		var amount float64
		switch v := row.Amount.(type) {
		case float64:
			amount = v
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				amount = f
			}
		}
		sums[row.Asset] += amount
	}

	result := make(map[string]string, len(sums))
	for asset, sum := range sums {
		result[asset] = strconv.FormatFloat(sum, 'f', 18, 64)
	}
	return result, nil
}

// SumFundingInRange sums ALL funding payments for an account within [fromMs, toMs].
// Unlike SumFundingForPosition, this does NOT filter by base_asset — it returns
// the total USDC-denominated funding received/paid across all perp markets.
// Used by the interest reconciler to account for perp funding in USDC balance changes.
func (c *Client) SumFundingInRange(ctx context.Context, accountID uuid.UUID, fromMs, toMs int64) (string, error) {
	query := `
		query SumFundingInRange(
			$exchange_account_id: uuid!
			$start_time: bigint!
			$end_time: bigint!
		) {
			funding_payments_aggregate(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
					timestamp: { _gt: $start_time, _lte: $end_time }
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

	vars := map[string]interface{}{
		"exchange_account_id": accountID.String(),
		"start_time":          fromMs,
		"end_time":            toMs,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		FundingPaymentsAggregate struct {
			Aggregate struct {
				Sum struct {
					Amount interface{} `json:"amount"`
				} `json:"sum"`
			} `json:"aggregate"`
		} `json:"funding_payments_aggregate"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return "0", fmt.Errorf("failed to sum funding in range: %w", err)
	}

	sum := resp.FundingPaymentsAggregate.Aggregate.Sum.Amount
	if sum == nil {
		return "0", nil
	}

	switch v := sum.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', 18, 64), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// PruneOldSpotBalanceSnapshots deletes spot_balance_snapshots older than retentionDays.
// Keeps data compact (at 5-min intervals: ~288 rows/asset/day → ~25,920 rows/asset/90 days).
func (c *Client) PruneOldSpotBalanceSnapshots(ctx context.Context, retentionMs int64) (int, error) {
	query := `
		mutation PruneOldSpotBalanceSnapshots($cutoff: bigint!) {
			delete_spot_balance_snapshots(
				where: { timestamp: { _lt: $cutoff } }
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"cutoff": retentionMs,
	})

	var resp struct {
		DeleteSpotBalanceSnapshots struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to prune old snapshots: %w", err)
	}

	return resp.DeleteSpotBalanceSnapshots.AffectedRows, nil
}
