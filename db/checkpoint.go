package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// ProcessorCheckpoint represents a saved processor state for incremental runs.
// Validity is determined by comparing per-type timestamps and schema_version
// against the current DB state.
type ProcessorCheckpoint struct {
	ExchangeAccountID       uuid.UUID            `json:"exchange_account_id"`
	State                   *models.AccountState `json:"state"`                     // Typed account state
	SchemaVersion           int                  `json:"schema_version"`            // Bumped when AccountState shape changes
	LastTradeTimestamp      int64                `json:"last_trade_timestamp"`      // Unix ms of newest trade
	LastTransferTimestamp   int64                `json:"last_transfer_timestamp"`   // Unix ms of newest transfer
	LastSettlementTimestamp int64                `json:"last_settlement_timestamp"` // Deprecated: kept for DB compat, no longer used for validation
	LastSnapshotTimestamp   int64                `json:"last_snapshot_timestamp"`   // Unix ms of newest snapshot
	LastError               *string              `json:"last_error"`                // Error message from the most recent failed run (NULL on success)
	UpdatedAt               string               `json:"updated_at"`
}

// UnmarshalJSON handles Hasura returning BIGINT as string and JSONB as escaped strings.
func (cp *ProcessorCheckpoint) UnmarshalJSON(data []byte) error {
	type Alias ProcessorCheckpoint
	aux := &struct {
		State                   interface{} `json:"state"`
		SchemaVersion           interface{} `json:"schema_version"`
		LastTradeTimestamp      interface{} `json:"last_trade_timestamp"`
		LastTransferTimestamp   interface{} `json:"last_transfer_timestamp"`
		LastSettlementTimestamp interface{} `json:"last_settlement_timestamp"`
		LastSnapshotTimestamp   interface{} `json:"last_snapshot_timestamp"`
		*Alias
	}{
		Alias: (*Alias)(cp),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle state: Hasura may return JSONB as escaped JSON string
	if aux.State != nil {
		var stateBytes []byte
		switch v := aux.State.(type) {
		case string:
			stateBytes = []byte(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to re-marshal state: %w", err)
			}
			stateBytes = b
		}
		cp.State = &models.AccountState{}
		if err := json.Unmarshal(stateBytes, cp.State); err != nil {
			return fmt.Errorf("failed to unmarshal account state: %w", err)
		}
	}

	// Handle BIGINT fields: Hasura returns these as strings
	cp.SchemaVersion = parseBigintToInt(aux.SchemaVersion)
	cp.LastTradeTimestamp = parseBigintToInt64(aux.LastTradeTimestamp)
	cp.LastTransferTimestamp = parseBigintToInt64(aux.LastTransferTimestamp)
	cp.LastSettlementTimestamp = parseBigintToInt64(aux.LastSettlementTimestamp)
	cp.LastSnapshotTimestamp = parseBigintToInt64(aux.LastSnapshotTimestamp)

	return nil
}

// parseBigintToInt64 converts Hasura's BIGINT (string or float64) to int64.
func parseBigintToInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int64(val)
	case string:
		var n int64
		fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

// parseBigintToInt converts Hasura's INT (string or float64) to int.
func parseBigintToInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		var n int
		fmt.Sscanf(val, "%d", &n)
		return n
	}
	return 0
}

// SaveCheckpoint upserts a processor checkpoint for an account.
// Takes a typed struct — lib handles JSON serialization internally.
func (c *Client) SaveCheckpoint(ctx context.Context, checkpoint *ProcessorCheckpoint) error {
	stateJSON, err := json.Marshal(checkpoint.State)
	if err != nil {
		return fmt.Errorf("failed to marshal account state: %w", err)
	}

	query := `
		mutation SaveCheckpoint($object: processor_checkpoints_insert_input!) {
			insert_processor_checkpoints_one(
				object: $object
				on_conflict: {
					constraint: processor_checkpoints_pkey
					update_columns: [state, schema_version, last_trade_timestamp, last_transfer_timestamp, last_settlement_timestamp, last_snapshot_timestamp, last_error, updated_at]
				}
			) {
				exchange_account_id
			}
		}
	`

	var lastError interface{}
	if checkpoint.LastError != nil {
		lastError = *checkpoint.LastError
	}

	object := map[string]interface{}{
		"exchange_account_id":       checkpoint.ExchangeAccountID.String(),
		"state":                     string(stateJSON),
		"schema_version":            checkpoint.SchemaVersion,
		"last_trade_timestamp":      checkpoint.LastTradeTimestamp,
		"last_transfer_timestamp":   checkpoint.LastTransferTimestamp,
		"last_settlement_timestamp": checkpoint.LastSettlementTimestamp,
		"last_snapshot_timestamp":   checkpoint.LastSnapshotTimestamp,
		"last_error":                lastError,
		"updated_at":                "now()",
	}

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"object": object,
	})

	var resp struct {
		InsertProcessorCheckpointsOne *struct {
			ExchangeAccountID string `json:"exchange_account_id"`
		} `json:"insert_processor_checkpoints_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	return nil
}

// LoadCheckpoint retrieves the processor checkpoint for an account.
// Returns nil if no checkpoint exists.
func (c *Client) LoadCheckpoint(ctx context.Context, accountID uuid.UUID) (*ProcessorCheckpoint, error) {
	query := `
		query LoadCheckpoint($id: uuid!) {
			processor_checkpoints_by_pk(exchange_account_id: $id) {
				exchange_account_id
				state
				schema_version
				last_trade_timestamp
				last_transfer_timestamp
				last_settlement_timestamp
				last_snapshot_timestamp
				last_error
				updated_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID.String(),
	})

	var resp struct {
		Checkpoint *ProcessorCheckpoint `json:"processor_checkpoints_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	return resp.Checkpoint, nil
}

// SetProcessorCheckpointError records an error message on the checkpoint row for an account.
// Upserts so that errors raised before the first successful SaveCheckpoint (e.g. after a
// force-replay that wipes the checkpoint and then errors during Phase A) still surface in
// processor_checkpoints.last_error rather than being log-only.
func (c *Client) SetProcessorCheckpointError(ctx context.Context, accountID uuid.UUID, errorMsg string) error {
	query := `
		mutation SetCheckpointError($object: processor_checkpoints_insert_input!) {
			insert_processor_checkpoints_one(
				object: $object
				on_conflict: {
					constraint: processor_checkpoints_pkey
					update_columns: [last_error, updated_at]
				}
			) {
				exchange_account_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"object": map[string]interface{}{
			"exchange_account_id": accountID.String(),
			"last_error":          errorMsg,
			"updated_at":          "now()",
		},
	})

	var resp struct {
		Insert *struct {
			ExchangeAccountID string `json:"exchange_account_id"`
		} `json:"insert_processor_checkpoints_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to set checkpoint error: %w", err)
	}

	return nil
}

// ClearProcessorCheckpointError sets last_error to NULL on the checkpoint row.
// No-op if no row exists.
func (c *Client) ClearProcessorCheckpointError(ctx context.Context, accountID uuid.UUID) error {
	query := `
		mutation ClearCheckpointError($id: uuid!) {
			update_processor_checkpoints_by_pk(
				pk_columns: {exchange_account_id: $id}
				_set: {last_error: null}
			) {
				exchange_account_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID.String(),
	})

	var resp struct {
		Update *struct {
			ExchangeAccountID string `json:"exchange_account_id"`
		} `json:"update_processor_checkpoints_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to clear checkpoint error: %w", err)
	}

	return nil
}

// DeleteCheckpoint removes the processor checkpoint for an account.
func (c *Client) DeleteCheckpoint(ctx context.Context, accountID uuid.UUID) error {
	query := `
		mutation DeleteCheckpoint($id: uuid!) {
			delete_processor_checkpoints_by_pk(exchange_account_id: $id) {
				exchange_account_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": accountID.String(),
	})

	var resp struct {
		Delete *struct {
			ExchangeAccountID string `json:"exchange_account_id"`
		} `json:"delete_processor_checkpoints_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	return nil
}
