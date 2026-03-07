package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// ProcessorCheckpoint represents a saved processor state for incremental runs
type ProcessorCheckpoint struct {
	ExchangeAccountID uuid.UUID       `json:"exchange_account_id"`
	State             json.RawMessage `json:"state"`               // Serialized AccountState
	LastEventTimestamp int64           `json:"last_event_timestamp"` // Unix ms of last processed event
	EventCounts       json.RawMessage `json:"event_counts"`        // {"trades": N, "funding": N, ...}
	UpdatedAt         string          `json:"updated_at"`
}

// UnmarshalJSON handles Hasura returning BIGINT as string and JSONB as escaped strings.
func (cp *ProcessorCheckpoint) UnmarshalJSON(data []byte) error {
	type Alias ProcessorCheckpoint
	aux := &struct {
		LastEventTimestamp interface{} `json:"last_event_timestamp"`
		State             interface{} `json:"state"`
		EventCounts       interface{} `json:"event_counts"`
		*Alias
	}{
		Alias: (*Alias)(cp),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle last_event_timestamp: Hasura returns BIGINT as string
	if aux.LastEventTimestamp != nil {
		switch v := aux.LastEventTimestamp.(type) {
		case float64:
			cp.LastEventTimestamp = int64(v)
		case string:
			_, err := fmt.Sscanf(v, "%d", &cp.LastEventTimestamp)
			if err != nil {
				return fmt.Errorf("failed to parse last_event_timestamp: %w", err)
			}
		}
	}

	// Handle state: Hasura may return JSONB as escaped JSON string
	if aux.State != nil {
		switch v := aux.State.(type) {
		case string:
			cp.State = json.RawMessage(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to re-marshal state: %w", err)
			}
			cp.State = json.RawMessage(b)
		}
	}

	// Handle event_counts: same JSONB-as-string issue
	if aux.EventCounts != nil {
		switch v := aux.EventCounts.(type) {
		case string:
			cp.EventCounts = json.RawMessage(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to re-marshal event_counts: %w", err)
			}
			cp.EventCounts = json.RawMessage(b)
		}
	}

	return nil
}

// SaveCheckpoint upserts a processor checkpoint for an account.
func (c *Client) SaveCheckpoint(ctx context.Context, accountID uuid.UUID, state json.RawMessage, lastEventTimestamp int64, eventCounts json.RawMessage) error {
	query := `
		mutation SaveCheckpoint($object: processor_checkpoints_insert_input!) {
			insert_processor_checkpoints_one(
				object: $object
				on_conflict: {
					constraint: processor_checkpoints_pkey
					update_columns: [state, last_event_timestamp, event_counts, updated_at]
				}
			) {
				exchange_account_id
			}
		}
	`

	object := map[string]interface{}{
		"exchange_account_id":  accountID.String(),
		"state":                string(state),
		"last_event_timestamp": lastEventTimestamp,
		"event_counts":         string(eventCounts),
		"updated_at":           "now()",
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
				last_event_timestamp
				event_counts
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
