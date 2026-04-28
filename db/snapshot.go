package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// BalanceSnapshot aliased from models (used by activity_processor for interest derivation)
type BalanceSnapshot = models.BalanceSnapshot

// SpotBalanceSnapshot aliased from models (DB record type)
type SpotBalanceSnapshot = models.SpotBalanceSnapshot

// SpotBalanceSnapshotInput aliased from models
type SpotBalanceSnapshotInput = models.SpotBalanceSnapshotInput

// GetLatestBalanceSnapshots returns the most recent balance snapshot per asset
// for the given account. It queries the spot_balance_snapshots table using
// distinct_on to get only the latest entry per asset.
// Returns string-based BalanceSnapshot (used by activity_processor for big.Rat math).
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
				timestamp
				wallet_type
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		Snapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest balance snapshots: %w", err)
	}

	balances := make([]*BalanceSnapshot, 0, len(resp.Snapshots))
	for _, s := range resp.Snapshots {
		balances = append(balances, &BalanceSnapshot{
			Asset:       s.Asset,
			Balance:     s.Balance,
			TimestampMs: s.Timestamp.UnixMilli(),
			WalletType:  s.WalletType,
		})
	}

	return balances, nil
}

// GetAllBalanceSnapshots returns balance snapshots for an account, sorted
// by timestamp ascending. Used by the activity processor for multi-point
// reconciliation — reconciling at every snapshot boundary during event replay.
// If afterTimestampMs > 0, only returns snapshots with timestamp > afterTimestampMs.
// Pass 0 to get all snapshots.
func (c *Client) GetAllBalanceSnapshots(ctx context.Context, accountID uuid.UUID, afterTimestampMs int64) ([]*BalanceSnapshot, error) {
	query := `
		query GetAllBalanceSnapshots($account_id: uuid!, $after_ms: bigint!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $account_id }
					timestamp: { _gt: $after_ms }
				}
				order_by: [{ timestamp: asc }, { asset: asc }]
			) {
				asset
				balance
				timestamp
				wallet_type
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"after_ms":   afterTimestampMs,
	})

	var resp struct {
		Snapshots []*SpotBalanceSnapshot `json:"spot_balance_snapshots"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get all balance snapshots: %w", err)
	}

	balances := make([]*BalanceSnapshot, 0, len(resp.Snapshots))
	for _, s := range resp.Snapshots {
		balances = append(balances, &BalanceSnapshot{
			Asset:       s.Asset,
			Balance:     s.Balance,
			TimestampMs: s.Timestamp.UnixMilli(),
			WalletType:  s.WalletType,
		})
	}

	return balances, nil
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
		// Default empty wallet_type to "spot" for backward compatibility —
		// callers that pre-date the wallet_type field still produce valid
		// rows. The DB column also has a "spot" default but we set it
		// explicitly here so the value is visible in the request payload.
		walletType := input.WalletType
		if walletType == "" {
			walletType = "spot"
		}
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"asset":               input.Asset,
			"balance":             input.Balance,
			"timestamp":           input.Timestamp.UnixMilli(),
			"wallet_type":         walletType,
		}
		objects[i] = obj
	}

	query := `
		mutation AddSpotBalanceSnapshots($objects: [spot_balance_snapshots_insert_input!]!) {
			insert_spot_balance_snapshots(objects: $objects) {
				returning {
					id
					exchange_account_id
					asset
					balance
					timestamp
					wallet_type
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
				timestamp
				wallet_type
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
				timestamp
				wallet_type
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

// ListSpotBalanceSnapshots returns all snapshots for a specific account and asset,
// sorted by timestamp ascending. Used by the interest reconciler to process all
// consecutive snapshot pairs when rebuilding historical interest data.
func (c *Client) ListSpotBalanceSnapshots(ctx context.Context, accountID uuid.UUID, asset string) ([]*SpotBalanceSnapshot, error) {
	query := `
		query ListSpotBalanceSnapshots($account_id: uuid!, $asset: String!) {
			spot_balance_snapshots(
				where: {
					exchange_account_id: { _eq: $account_id }
					asset: { _eq: $asset }
				}
				order_by: { timestamp: asc }
			) {
				id
				exchange_account_id
				asset
				balance
				timestamp
				wallet_type
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
		return nil, fmt.Errorf("failed to list spot balance snapshots: %w", err)
	}

	return resp.Snapshots, nil
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

	// Pass metadata as a raw JSON value (not string) to avoid double-encoding.
	// The GraphQL library JSON-encodes variables, so passing string(metadata)
	// would produce a double-encoded JSON string in the JSONB column.
	var metadataValue interface{}
	if err := json.Unmarshal(metadata, &metadataValue); err != nil {
		return fmt.Errorf("failed to parse metadata JSON: %w", err)
	}
	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":       accountID.String(),
		"metadata": metadataValue,
	})

	var resp struct {
		Update *struct {
			ID string `json:"id"`
		} `json:"update_exchange_accounts_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to update account type metadata: %w", err)
	}

	if resp.Update == nil {
		return notFoundError("account", accountID.String())
	}

	return nil
}

// GetDistinctTransferAssets returns distinct asset names from transfers for an account.
func (c *Client) GetDistinctTransferAssets(ctx context.Context, accountID uuid.UUID) ([]string, error) {
	query := `
		query GetDistinctTransferAssets($account_id: uuid!) {
			transfers(
				where: { exchange_account_id: { _eq: $account_id } }
				distinct_on: asset
			) {
				asset
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
	})

	var resp struct {
		Transfers []struct {
			Asset string `json:"asset"`
		} `json:"transfers"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get distinct transfer assets: %w", err)
	}

	assets := make([]string, 0, len(resp.Transfers))
	for _, t := range resp.Transfers {
		if t.Asset != "" {
			assets = append(assets, t.Asset)
		}
	}

	return assets, nil
}
