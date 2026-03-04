package db

import (
	"context"
	"fmt"
	"math/big"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/models"
)

// Transfer aliased from models
type Transfer = models.Transfer

// TransferFilter aliased from models
type TransferFilter = models.TransferFilter

// ListTransfers returns unified transfer events by querying deposits + interest_payments.
// Results are NOT sorted — caller is responsible for ordering.
func (c *Client) ListTransfers(ctx context.Context, filter TransferFilter) ([]*Transfer, error) {
	var transfers []*Transfer

	// 1. Load deposits (deposit/withdraw events)
	depositFilter := DepositFilter{}
	if len(filter.ExchangeAccountIDs) > 0 {
		depositFilter.ExchangeAccountIDs = filter.ExchangeAccountIDs
	}

	deposits, err := c.ListDeposits(ctx, depositFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to list deposits for transfers: %w", err)
	}

	for _, d := range deposits {
		transferType := models.TypeDeposit
		if d.Direction == "withdraw" {
			transferType = models.TypeWithdraw
		}
		transfers = append(transfers, &Transfer{
			ID:                d.ID,
			ExchangeAccountID: d.ExchangeAccountID,
			Type:              transferType,
			Asset:             d.Asset,
			Amount:            absNumeric(d.Amount),
			Timestamp:         d.Timestamp,
		})
	}

	// 2. Load interest payments
	if len(filter.ExchangeAccountIDs) > 0 {
		for _, accountID := range filter.ExchangeAccountIDs {
			interestPayments, err := c.listInterestPaymentsForAccount(ctx, accountID)
			if err != nil {
				continue
			}

			for _, ip := range interestPayments {
				transfers = append(transfers, &Transfer{
					ID:                ip.ID,
					ExchangeAccountID: ip.ExchangeAccountID,
					Type:              models.TypeInterest,
					Asset:             ip.Asset,
					Amount:            ip.Amount,
					Timestamp:         ip.Timestamp,
				})
			}
		}
	}

	return transfers, nil
}

// listInterestPaymentsForAccount fetches all interest payment records for an account
func (c *Client) listInterestPaymentsForAccount(ctx context.Context, accountID uuid.UUID) ([]*InterestPayment, error) {
	query := `
		query ListInterestPayments($exchange_account_id: uuid!) {
			interest_payments(
				where: {
					exchange_account_id: { _eq: $exchange_account_id }
				}
				order_by: { timestamp: asc }
			) {
				id
				exchange_account_id
				asset
				amount
				timestamp
				snapshot_from
				snapshot_to
				is_approximate
				created_at
			}
		}
	`

	req := c.graphqlRequestWithVars(query, map[string]interface{}{
		"exchange_account_id": accountID.String(),
	})

	var resp struct {
		InterestPayments []*InterestPayment `json:"interest_payments"`
	}

	if err := c.execute(ctx, req, &resp); err != nil {
		return nil, fmt.Errorf("failed to list interest payments: %w", err)
	}

	return resp.InterestPayments, nil
}

// absNumeric returns the absolute value of a numeric string
func absNumeric(s string) string {
	f, _, err := big.ParseFloat(s, 10, 256, big.ToNearestEven)
	if err != nil || f == nil {
		return s
	}
	return f.Abs(f).Text('f', 18)
}
