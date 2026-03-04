package db

import (
	"context"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// SumFundingForPosition sums all funding payments for a given account and base asset
// within the inclusive time range [startTimeMs, endTimeMs] (Unix milliseconds).
// Returns the net funding as a decimal string ("0" if none found).
// Positive = received, negative = paid (matches funding_payments.amount sign convention).
func (c *Client) SumFundingForPosition(
	ctx context.Context,
	accountID uuid.UUID,
	baseAsset string,
	startTimeMs, endTimeMs int64,
) (string, error) {
	query := `
		query SumFundingForPosition(
			$exchange_account_id: uuid!
			$base_asset: String!
			$start_time: bigint!
			$end_time: bigint!
		) {
			funding_payments_aggregate(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
					base_asset: { _eq: $base_asset }
					timestamp: { _gte: $start_time, _lte: $end_time }
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
		"base_asset":          baseAsset,
		"start_time":          startTimeMs,
		"end_time":            endTimeMs,
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
		return "0", fmt.Errorf("failed to sum funding for position: %w", err)
	}

	sum := resp.FundingPaymentsAggregate.Aggregate.Sum.Amount
	if sum == nil {
		return "0", nil
	}

	// Convert the sum to a string (Hasura returns NUMERIC aggregates as float64 or string)
	switch v := sum.(type) {
	case string:
		return v, nil
	case float64:
		return strconv.FormatFloat(v, 'f', 18, 64), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// FundingPaymentFilter represents filtering options for listing funding payments
type FundingPaymentFilter struct {
	ExchangeAccountIDs []uuid.UUID
}

// FundingPayment represents a funding payment model (aliased from models package)
type FundingPayment = models.FundingPayment

// FundingPaymentInput represents funding payment input for mutations (aliased from models package)
type FundingPaymentInput = models.FundingPaymentInput

// ListFundingPayments retrieves funding payments with optional filtering
func (c *Client) ListFundingPayments(ctx context.Context, filter FundingPaymentFilter) ([]*FundingPayment, error) {
	vars := make(map[string]interface{})
	whereClause := ""
	varDeclarations := ""

	if len(filter.ExchangeAccountIDs) > 0 {
		accountIDs := make([]string, len(filter.ExchangeAccountIDs))
		for i, id := range filter.ExchangeAccountIDs {
			accountIDs[i] = id.String()
		}
		vars["account_ids"] = accountIDs
		whereClause = "where: { exchange_account_id: { _in: $account_ids } }"
		varDeclarations = "$account_ids: [uuid!]"
	}

	queryHeader := "query ListFundingPayments"
	if varDeclarations != "" {
		queryHeader += "(" + varDeclarations + ")"
	}

	query := fmt.Sprintf(`
		%s {
			funding_payments(
				%s
				order_by: { timestamp: desc }
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
	`, queryHeader, whereClause)

	req := c.graphqlRequestWithVars(query, vars)

	var resp struct {
		FundingPayments []*FundingPayment `json:"funding_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list funding payments: %w", err)
	}

	return resp.FundingPayments, nil
}

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
		return nil, fmt.Errorf("failed to add funding payments: %w", err)
	}

	return resp.InsertFundingPayments.Returning, nil
}
