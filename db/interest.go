package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// InterestPayment aliased from models
type InterestPayment = models.InterestPayment

// InterestPaymentInput aliased from models
type InterestPaymentInput = models.InterestPaymentInput

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

	query := `
		mutation AddInterestPayments($objects: [interest_payments_insert_input!]!) {
			insert_interest_payments(
				objects: $objects
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

// DeleteInterestPaymentsForAccount deletes all interest payment records for a given account.
// Used by the activity processor to ensure idempotent re-derivation on each run.
func (c *Client) DeleteInterestPaymentsForAccount(ctx context.Context, accountID uuid.UUID) (int, error) {
	query := `
		mutation DeleteInterestPayments($exchange_account_id: uuid!) {
			delete_interest_payments(
				where: { exchange_account_id: { _eq: $exchange_account_id } }
			) {
				affected_rows
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
	})

	var resp struct {
		DeleteInterestPayments struct {
			AffectedRows int `json:"affected_rows"`
		} `json:"delete_interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return 0, fmt.Errorf("failed to delete interest payments: %w", err)
	}

	return resp.DeleteInterestPayments.AffectedRows, nil
}
