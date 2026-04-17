package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/zif-terminal/lib/models"
)

// DiscoverAccounts discovers Lighter accounts for a given Ethereum L1 address.
// Uses the public GET /api/v1/account endpoint (no auth needed).
// Returns accounts with status "needs_token" since API key is required for data access.
func (c *Client) DiscoverAccounts(ctx context.Context, userIdentifier string) ([]*models.DiscoverableAccount, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier (Ethereum L1 address) is required")
	}

	url := fmt.Sprintf("%s/account?by=l1_address&value=%s", c.baseURL, userIdentifier)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account: %w", err)
	}

	if body == nil {
		return nil, nil
	}

	var resp lighterAccountResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode account response: %w", err)
	}

	// The Lighter API returns HTTP 200 for all responses but uses an internal code field.
	// Code 21100 means "account not found". Any non-200 code means no valid account.
	if resp.Code != 200 || len(resp.Accounts) == 0 {
		return nil, nil
	}

	accounts := make([]*models.DiscoverableAccount, 0, len(resp.Accounts))
	for _, acct := range resp.Accounts {
		name := "Main Account"
		accountType := "main"
		if len(resp.Accounts) > 1 {
			name = fmt.Sprintf("Account %d", acct.AccountIndex)
			if acct.AccountIndex > 0 {
				accountType = "sub_account"
			}
		}

		accounts = append(accounts, &models.DiscoverableAccount{
			AccountIdentifier: strconv.Itoa(acct.AccountIndex),
			AccountType:       accountType,
			Name:              name,
			Metadata: map[string]interface{}{
				"l1_address":    userIdentifier,
				"account_index": acct.AccountIndex,
			},
		})
	}

	return accounts, nil
}
