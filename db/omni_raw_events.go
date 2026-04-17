package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OmniRawEvent represents a row in the omni_raw_events staging table.
type OmniRawEvent struct {
	ID                    uuid.UUID       `json:"id"`
	ExchangeAccountID     uuid.UUID       `json:"exchange_account_id"`
	OmniID                string          `json:"omni_id"`
	CreatedAt             time.Time       `json:"created_at"`
	TimestampMs           int64           `json:"timestamp_ms"`
	EventType             string          `json:"event_type"`
	Side                  *string         `json:"side,omitempty"`
	InstrumentType        *string         `json:"instrument_type,omitempty"`
	Underlying            *string         `json:"underlying,omitempty"`
	Price                 *string         `json:"price,omitempty"`
	Qty                   *string         `json:"qty,omitempty"`
	TradeType             *string         `json:"trade_type,omitempty"`
	LiquidationTriggerPrice *string       `json:"liquidation_trigger_price,omitempty"`
	Asset                 *string         `json:"asset,omitempty"`
	TransferType          *string         `json:"transfer_type,omitempty"`
	FeeType               *string         `json:"fee_type,omitempty"`
	FundingRate           *string         `json:"funding_rate,omitempty"`
	Status                *string         `json:"status,omitempty"`
	RawData               json.RawMessage `json:"raw_data"`
	UploadBatchID         uuid.UUID       `json:"upload_batch_id"`
	UploadedAt            time.Time       `json:"uploaded_at"`
}

// OmniRawEventInput represents input for inserting an omni_raw_event.
type OmniRawEventInput struct {
	ExchangeAccountID     uuid.UUID       `json:"exchange_account_id"`
	OmniID                string          `json:"omni_id"`
	CreatedAt             time.Time       `json:"created_at"`
	TimestampMs           int64           `json:"timestamp_ms"`
	EventType             string          `json:"event_type"`
	Side                  *string         `json:"side,omitempty"`
	InstrumentType        *string         `json:"instrument_type,omitempty"`
	Underlying            *string         `json:"underlying,omitempty"`
	Price                 *string         `json:"price,omitempty"`
	Qty                   *string         `json:"qty,omitempty"`
	TradeType             *string         `json:"trade_type,omitempty"`
	LiquidationTriggerPrice *string       `json:"liquidation_trigger_price,omitempty"`
	Asset                 *string         `json:"asset,omitempty"`
	TransferType          *string         `json:"transfer_type,omitempty"`
	FeeType               *string         `json:"fee_type,omitempty"`
	FundingRate           *string         `json:"funding_rate,omitempty"`
	Status                *string         `json:"status,omitempty"`
	RawData               json.RawMessage `json:"raw_data"`
	UploadBatchID         uuid.UUID       `json:"upload_batch_id"`
}

// omniRawEventFields is the GraphQL field list for omni_raw_events queries.
const omniRawEventFields = `
	id
	exchange_account_id
	omni_id
	created_at
	timestamp_ms
	event_type
	side
	instrument_type
	underlying
	price
	qty
	trade_type
	liquidation_trigger_price
	asset
	transfer_type
	fee_type
	funding_rate
	status
	raw_data
	upload_batch_id
	uploaded_at
`

// ListOmniRawEvents queries omni_raw_events by account, event_type, and optional
// minimum timestamp (inclusive). Results are ordered by timestamp_ms ascending.
func (c *Client) ListOmniRawEvents(ctx context.Context, accountID uuid.UUID, eventType string, sinceMs int64) ([]*OmniRawEvent, error) {
	query := fmt.Sprintf(`
		query ListOmniRawEvents($account_id: uuid!, $event_type: String!, $since_ms: bigint!) {
			omni_raw_events(
				where: {
					exchange_account_id: { _eq: $account_id }
					event_type: { _eq: $event_type }
					timestamp_ms: { _gte: $since_ms }
				}
				order_by: { timestamp_ms: asc }
			) {
				%s
			}
		}
	`, omniRawEventFields)

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": accountID.String(),
		"event_type": eventType,
		"since_ms":   sinceMs,
	})

	var resp struct {
		OmniRawEvents []*OmniRawEvent `json:"omni_raw_events"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list omni raw events: %w", err)
	}

	return resp.OmniRawEvents, nil
}

// ListOmniRawEventsByTypes queries omni_raw_events by account and multiple event types,
// with optional minimum timestamp. Results are ordered by timestamp_ms ascending.
func (c *Client) ListOmniRawEventsByTypes(ctx context.Context, accountID uuid.UUID, eventTypes []string, sinceMs int64) ([]*OmniRawEvent, error) {
	query := fmt.Sprintf(`
		query ListOmniRawEventsByTypes($account_id: uuid!, $event_types: [String!]!, $since_ms: bigint!) {
			omni_raw_events(
				where: {
					exchange_account_id: { _eq: $account_id }
					event_type: { _in: $event_types }
					timestamp_ms: { _gte: $since_ms }
				}
				order_by: { timestamp_ms: asc }
			) {
				%s
			}
		}
	`, omniRawEventFields)

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id":  accountID.String(),
		"event_types": eventTypes,
		"since_ms":    sinceMs,
	})

	var resp struct {
		OmniRawEvents []*OmniRawEvent `json:"omni_raw_events"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list omni raw events by types: %w", err)
	}

	return resp.OmniRawEvents, nil
}

// AddOmniRawEvents bulk-inserts omni_raw_event records. Uses ON CONFLICT DO NOTHING
// on the (exchange_account_id, omni_id) unique constraint to skip duplicates.
// Returns the number of rows actually inserted.
func (c *Client) AddOmniRawEvents(ctx context.Context, inputs []*OmniRawEventInput) (int, error) {
	if len(inputs) == 0 {
		return 0, nil
	}

	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		obj := map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"omni_id":             input.OmniID,
			"created_at":          input.CreatedAt.Format(time.RFC3339Nano),
			"timestamp_ms":        input.TimestampMs,
			"event_type":          input.EventType,
			"raw_data":            input.RawData,
			"upload_batch_id":     input.UploadBatchID.String(),
		}
		if input.Side != nil {
			obj["side"] = *input.Side
		}
		if input.InstrumentType != nil {
			obj["instrument_type"] = *input.InstrumentType
		}
		if input.Underlying != nil {
			obj["underlying"] = *input.Underlying
		}
		if input.Price != nil {
			obj["price"] = *input.Price
		}
		if input.Qty != nil {
			obj["qty"] = *input.Qty
		}
		if input.TradeType != nil {
			obj["trade_type"] = *input.TradeType
		}
		if input.LiquidationTriggerPrice != nil {
			obj["liquidation_trigger_price"] = *input.LiquidationTriggerPrice
		}
		if input.Asset != nil {
			obj["asset"] = *input.Asset
		}
		if input.TransferType != nil {
			obj["transfer_type"] = *input.TransferType
		}
		if input.FeeType != nil {
			obj["fee_type"] = *input.FeeType
		}
		if input.FundingRate != nil {
			obj["funding_rate"] = *input.FundingRate
		}
		if input.Status != nil {
			obj["status"] = *input.Status
		}
		objects[i] = obj
	}

	query := `
		mutation AddOmniRawEvents($objects: [omni_raw_events_insert_input!]!) {
			insert_omni_raw_events(
				objects: $objects
				on_conflict: {
					constraint: omni_raw_events_exchange_account_id_omni_id_key
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
		InsertOmniRawEvents struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"insert_omni_raw_events"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to add omni raw events: %w", err)
	}

	return resp.InsertOmniRawEvents.AffectedRows, nil
}
