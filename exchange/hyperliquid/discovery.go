package hyperliquid

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/zif-terminal/lib/models"
)

// DiscoverAccounts discovers Hyperliquid accounts for a given Ethereum wallet address.
// Checks if the address has any perp positions or spot balances indicating activity.
func (c *Client) DiscoverAccounts(ctx context.Context, userIdentifier string) ([]*models.DiscoverableAccount, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier (Ethereum wallet address) is required")
	}

	// Check perp clearinghouse state
	var perpState hlClearinghouseState
	err := c.doPost(ctx, map[string]string{
		"type": "clearinghouseState",
		"user": userIdentifier,
	}, &perpState)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clearinghouse state: %w", err)
	}

	// Check spot clearinghouse state
	var spotState hlSpotClearinghouseState
	err = c.doPost(ctx, map[string]string{
		"type": "spotClearinghouseState",
		"user": userIdentifier,
	}, &spotState)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spot clearinghouse state: %w", err)
	}

	// Determine if the account has any activity
	hasActivity := false

	// Check if account value is non-zero
	if accountValue, err := strconv.ParseFloat(perpState.MarginSummary.AccountValue, 64); err == nil && math.Abs(accountValue) > 0.01 {
		hasActivity = true
	}

	// Check if there are any perp positions
	if !hasActivity {
		for _, ap := range perpState.AssetPositions {
			if szi, err := strconv.ParseFloat(ap.Position.Szi, 64); err == nil && math.Abs(szi) > 0 {
				hasActivity = true
				break
			}
		}
	}

	// Check if there are any spot balances
	if !hasActivity {
		for _, b := range spotState.Balances {
			if total, err := strconv.ParseFloat(b.Total, 64); err == nil && math.Abs(total) > 0.01 {
				hasActivity = true
				break
			}
		}
	}

	if !hasActivity {
		return nil, nil
	}

	// Hyperliquid uses the wallet address directly (no subaccounts in the Drift sense)
	return []*models.DiscoverableAccount{
		{
			AccountIdentifier: userIdentifier,
			AccountType:       "main",
			Name:              "Main Account",
			Metadata: map[string]interface{}{
				"wallet": userIdentifier,
			},
		},
	}, nil
}
