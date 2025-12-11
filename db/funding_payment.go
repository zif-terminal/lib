package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// FundingPayment represents a funding payment model (aliased from models package)
type FundingPayment = models.FundingPayment

// FundingPaymentInput represents funding payment input for mutations (aliased from models package)
type FundingPaymentInput = models.FundingPaymentInput

// GetLatestFundingPayment retrieves the latest funding payment for an exchange account
func (c *Client) GetLatestFundingPayment(ctx context.Context, exchangeAccountID uuid.UUID) (*FundingPayment, error) {
	query := `
		query GetLatestFundingPayment($exchange_account_id: uuid!) {
			funding_payments(
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
				base_asset
				quote_asset
				amount
				timestamp
				payment_id
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": exchangeAccountID.String(),
	})

	var resp struct {
		FundingPayments []*FundingPayment `json:"funding_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get latest funding payment: %w", err)
	}

	if len(resp.FundingPayments) == 0 {
		return nil, nil // No funding payment found - return nil, no error
	}

	return resp.FundingPayments[0], nil
}

// AddFundingPayments adds one or many funding payments
// Uses batch insert for all cases (even single payment)
// Handles duplicate errors gracefully and continues inserting remaining payments
func (c *Client) AddFundingPayments(ctx context.Context, inputs []*FundingPaymentInput) ([]*FundingPayment, error) {
	if len(inputs) == 0 {
		return []*FundingPayment{}, nil
	}

	// Convert inputs to GraphQL format
	objects := make([]map[string]interface{}, len(inputs))
	for i, input := range inputs {
		objects[i] = map[string]interface{}{
			"exchange_account_id": input.ExchangeAccountID.String(),
			"base_asset":          input.BaseAsset,
			"quote_asset":         input.QuoteAsset,
			"amount":              input.Amount,
			"timestamp":           input.Timestamp.UnixMilli(),
			"payment_id":          input.PaymentID,
		}
	}

	// Always use batch insert, even for single payment
	query := `
		mutation AddFundingPayments($objects: [funding_payments_insert_input!]!) {
			insert_funding_payments(objects: $objects) {
				returning {
					id
					exchange_account_id
					base_asset
					quote_asset
					amount
					timestamp
					payment_id
				}
			}
		}
	`

	vars := map[string]interface{}{
		"objects": objects,
	}

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		InsertFundingPayments struct {
			Returning []*FundingPayment `json:"returning"`
		} `json:"insert_funding_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		// For batch inserts, check if it's a duplicate error
		// If so, try inserting individually to handle partial success
		if isDuplicateError(err) {
			return c.addFundingPaymentsIndividually(ctx, inputs)
		}
		return nil, fmt.Errorf("failed to add funding payments: %w", err)
	}

	return resp.InsertFundingPayments.Returning, nil
}

// addFundingPaymentsIndividually inserts funding payments one by one
// Used when batch insert fails due to duplicates, to handle partial success
func (c *Client) addFundingPaymentsIndividually(ctx context.Context, inputs []*FundingPaymentInput) ([]*FundingPayment, error) {
	var results []*FundingPayment
	var errors []string

	for _, input := range inputs {
		payments, err := c.AddFundingPayments(ctx, []*FundingPaymentInput{input})
		if err != nil {
			errors = append(errors, fmt.Sprintf("payment_id=%s: %v", input.PaymentID, err))
			continue
		}
		if len(payments) > 0 {
			results = append(results, payments...)
		}
		// If payment is duplicate, len(payments) == 0, which is fine - we continue
	}

	// Return successfully inserted payments even if some failed
	// This allows partial success
	return results, nil
}

// isDuplicateError checks if an error is a unique constraint violation
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Common patterns for duplicate/unique constraint errors
	return strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "unique constraint") ||
		strings.Contains(errStr, "violates unique constraint") ||
		strings.Contains(errStr, "already exists")
}
