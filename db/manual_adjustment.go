package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ManualAdjustment is an operator-authored, raw exchange-format event payload
// that account_sync merges into the real-data feed for a single
// (exchange_account_id, exchange) before the per-exchange transformer runs.
//
// The transformer treats an adjustment payload identically to a real exchange
// response — the same code that produces a Transfer / Trade from a real
// hlFundingEntry produces one from an adjustment-sourced hlFundingEntry — and
// tags the resulting row with metadata.manual_adjustment_id for audit.
//
// Adjustments are SOFT-DELETED via deactivated_at; downstream detectors
// (R2 / R3 / R4) are NOT aware of this table. They verify the resulting
// Transfer rows like any other event, which is how the no-synthetic-
// reconciliation rule is preserved: nothing in the processor path
// auto-generates these — an operator wrote a row after debugging the gap.
type ManualAdjustment struct {
	ID                uuid.UUID
	ExchangeAccountID uuid.UUID
	Exchange          string
	EventType         string
	Payload           json.RawMessage
	Reason            string
	CreatedBy         string
	CreatedAt         time.Time
	// DeactivatedAt is nil while the row is active; non-nil after an operator
	// deactivates it (e.g. because the exchange later backfilled the data).
	DeactivatedAt     *time.Time
	DeactivatedReason string
}

// ListAdjustmentsFilter controls which manual_adjustments ListManualAdjustments returns.
// Nil/empty fields mean "don't filter on this dimension".
type ListAdjustmentsFilter struct {
	// ExchangeAccountID, if non-zero, restricts to a single account.
	ExchangeAccountID uuid.UUID
	// Exchange, if non-empty, restricts to a single exchange (e.g. "hyperliquid").
	Exchange string
	// EventType, if non-empty, restricts to a single event type.
	EventType string
	// IncludeDeactivated=true includes soft-deleted rows. Default is active-only.
	IncludeDeactivated bool
}

// adjustmentRow is the on-the-wire shape returned by Hasura.
// Kept private; callers see *ManualAdjustment.
type adjustmentRow struct {
	ID                string          `json:"id"`
	ExchangeAccountID string          `json:"exchange_account_id"`
	Exchange          string          `json:"exchange"`
	EventType         string          `json:"event_type"`
	Payload           json.RawMessage `json:"payload"`
	Reason            string          `json:"reason"`
	CreatedBy         *string         `json:"created_by"`
	CreatedAt         time.Time       `json:"created_at"`
	DeactivatedAt     *time.Time      `json:"deactivated_at"`
	DeactivatedReason *string         `json:"deactivated_reason"`
}

func (r adjustmentRow) toModel() (*ManualAdjustment, error) {
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid adjustment id %q: %w", r.ID, err)
	}
	accountID, err := uuid.Parse(r.ExchangeAccountID)
	if err != nil {
		return nil, fmt.Errorf("invalid exchange_account_id %q: %w", r.ExchangeAccountID, err)
	}
	a := &ManualAdjustment{
		ID:                id,
		ExchangeAccountID: accountID,
		Exchange:          r.Exchange,
		EventType:         r.EventType,
		Payload:           r.Payload,
		Reason:            r.Reason,
		CreatedAt:         r.CreatedAt,
		DeactivatedAt:     r.DeactivatedAt,
	}
	if r.CreatedBy != nil {
		a.CreatedBy = *r.CreatedBy
	}
	if r.DeactivatedReason != nil {
		a.DeactivatedReason = *r.DeactivatedReason
	}
	return a, nil
}

const adjustmentSelectFields = `
	id
	exchange_account_id
	exchange
	event_type
	payload
	reason
	created_by
	created_at
	deactivated_at
	deactivated_reason
`

// FetchActiveManualAdjustments returns all rows for the given
// (exchange_account_id, exchange) that have NOT been deactivated. This is the
// hot path called by the account_sync orchestrator at the start of every
// sync cycle.
//
// Results are ordered by created_at ASC so the merge with real data is
// deterministic and any cross-row ordering established by the operator is
// preserved.
func (c *Client) FetchActiveManualAdjustments(ctx context.Context, exchangeAccountID uuid.UUID, exchange string) ([]*ManualAdjustment, error) {
	query := `
		query FetchActiveManualAdjustments($account_id: uuid!, $exchange: String!) {
			manual_adjustments(
				where: {
					exchange_account_id: { _eq: $account_id }
					exchange: { _eq: $exchange }
					deactivated_at: { _is_null: true }
				}
				order_by: { created_at: asc }
			) {
				` + adjustmentSelectFields + `
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"account_id": exchangeAccountID.String(),
		"exchange":   exchange,
	})

	var resp struct {
		ManualAdjustments []adjustmentRow `json:"manual_adjustments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to fetch active manual adjustments for %s/%s: %w", exchangeAccountID, exchange, err)
	}

	out := make([]*ManualAdjustment, 0, len(resp.ManualAdjustments))
	for _, r := range resp.ManualAdjustments {
		m, err := r.toModel()
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// InsertManualAdjustment inserts a new row. The caller is responsible for
// having validated the Payload field decodes as the expected event_type
// struct — this method does NOT validate payload shape so it can be reused
// by future exchanges/event_types without growing into a giant switch.
//
// Returns the generated UUID.
func (c *Client) InsertManualAdjustment(ctx context.Context, adj *ManualAdjustment) (uuid.UUID, error) {
	if adj == nil {
		return uuid.Nil, fmt.Errorf("nil adjustment")
	}
	if adj.ExchangeAccountID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("exchange_account_id is required")
	}
	if adj.Exchange == "" {
		return uuid.Nil, fmt.Errorf("exchange is required")
	}
	if adj.EventType == "" {
		return uuid.Nil, fmt.Errorf("event_type is required")
	}
	if len(adj.Payload) == 0 {
		return uuid.Nil, fmt.Errorf("payload is required")
	}
	if adj.Reason == "" {
		return uuid.Nil, fmt.Errorf("reason is required (every adjustment must document why it exists)")
	}

	// Payload arrives as json.RawMessage; pass it as a parsed value so Hasura
	// stores it as jsonb (sending the raw bytes as a string would double-encode).
	var payloadValue interface{}
	if err := json.Unmarshal(adj.Payload, &payloadValue); err != nil {
		return uuid.Nil, fmt.Errorf("payload is not valid JSON: %w", err)
	}

	obj := map[string]interface{}{
		"exchange_account_id": adj.ExchangeAccountID.String(),
		"exchange":            adj.Exchange,
		"event_type":          adj.EventType,
		"payload":             payloadValue,
		"reason":              adj.Reason,
	}
	if adj.CreatedBy != "" {
		obj["created_by"] = adj.CreatedBy
	}

	query := `
		mutation InsertManualAdjustment($object: manual_adjustments_insert_input!) {
			insert_manual_adjustments_one(object: $object) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"object": obj,
	})

	var resp struct {
		InsertManualAdjustmentsOne *struct {
			ID string `json:"id"`
		} `json:"insert_manual_adjustments_one"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return uuid.Nil, fmt.Errorf("failed to insert manual adjustment: %w", err)
	}
	if resp.InsertManualAdjustmentsOne == nil {
		return uuid.Nil, fmt.Errorf("insert manual adjustment returned no row")
	}

	id, err := uuid.Parse(resp.InsertManualAdjustmentsOne.ID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid id returned by insert: %w", err)
	}
	return id, nil
}

// DeactivateManualAdjustment soft-deletes the row by setting
// deactivated_at = now() and deactivated_reason = reason. Returns ErrNotFound
// if the row doesn't exist.
func (c *Client) DeactivateManualAdjustment(ctx context.Context, id uuid.UUID, reason string) error {
	if reason == "" {
		return fmt.Errorf("deactivation reason is required")
	}

	query := `
		mutation DeactivateManualAdjustment($id: uuid!, $reason: String!, $ts: timestamptz!) {
			update_manual_adjustments_by_pk(
				pk_columns: { id: $id }
				_set: { deactivated_at: $ts, deactivated_reason: $reason }
			) {
				id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id":     id.String(),
		"reason": reason,
		"ts":     "now()",
	})

	var resp struct {
		UpdateManualAdjustmentsByPk *struct {
			ID string `json:"id"`
		} `json:"update_manual_adjustments_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return fmt.Errorf("failed to deactivate manual adjustment %s: %w", id, err)
	}
	if resp.UpdateManualAdjustmentsByPk == nil {
		return notFoundError("manual_adjustment", id.String())
	}
	return nil
}

// GetManualAdjustment fetches a single row by primary key. Returns ErrNotFound
// if the row doesn't exist.
func (c *Client) GetManualAdjustment(ctx context.Context, id uuid.UUID) (*ManualAdjustment, error) {
	query := `
		query GetManualAdjustment($id: uuid!) {
			manual_adjustments_by_pk(id: $id) {
				` + adjustmentSelectFields + `
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"id": id.String(),
	})

	var resp struct {
		ManualAdjustmentsByPk *adjustmentRow `json:"manual_adjustments_by_pk"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get manual adjustment %s: %w", id, err)
	}
	if resp.ManualAdjustmentsByPk == nil {
		return nil, notFoundError("manual_adjustment", id.String())
	}
	return resp.ManualAdjustmentsByPk.toModel()
}

// ListManualAdjustments returns rows matching the filter, ordered by
// created_at DESC (newest first — operator-facing list).
func (c *Client) ListManualAdjustments(ctx context.Context, filter ListAdjustmentsFilter) ([]*ManualAdjustment, error) {
	where := map[string]interface{}{}
	if filter.ExchangeAccountID != uuid.Nil {
		where["exchange_account_id"] = map[string]interface{}{"_eq": filter.ExchangeAccountID.String()}
	}
	if filter.Exchange != "" {
		where["exchange"] = map[string]interface{}{"_eq": filter.Exchange}
	}
	if filter.EventType != "" {
		where["event_type"] = map[string]interface{}{"_eq": filter.EventType}
	}
	if !filter.IncludeDeactivated {
		where["deactivated_at"] = map[string]interface{}{"_is_null": true}
	}

	query := `
		query ListManualAdjustments($where: manual_adjustments_bool_exp!) {
			manual_adjustments(
				where: $where
				order_by: { created_at: desc }
			) {
				` + adjustmentSelectFields + `
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"where": where,
	})

	var resp struct {
		ManualAdjustments []adjustmentRow `json:"manual_adjustments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list manual adjustments: %w", err)
	}

	out := make([]*ManualAdjustment, 0, len(resp.ManualAdjustments))
	for _, r := range resp.ManualAdjustments {
		m, err := r.toModel()
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
