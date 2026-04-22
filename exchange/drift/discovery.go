package drift

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// FetchAccountName is a no-op stub for Drift.
//
// Drift does not expose subaccount names via its public data API — the name
// field visible in the Drift UI is stored on-chain inside each subaccount's
// UserAccount struct, which requires RPC access to the Solana program. Pulling
// it is out of scope for discovery-time label population, so we return "".
//
// If/when on-chain name fetching is added, this method is the single place
// to wire it up without touching the detector.
func (c *Client) FetchAccountName(ctx context.Context, account *models.ExchangeAccount) (string, error) {
	return "", nil
}

// DiscoverAccounts discovers all syncable accounts for a given Solana wallet address
// Drift uses subaccounts tied to a wallet authority
// GET /authority/{walletAddress}/accounts
func (c *Client) DiscoverAccounts(ctx context.Context, userIdentifier string) ([]*models.DiscoverableAccount, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier (Solana wallet address) is required")
	}

	url := fmt.Sprintf("%s/authority/%s/accounts", c.baseURL, userIdentifier)

	// Use doRequestWithRetry which handles 403/429 with exponential backoff
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, resp.Status)
	}

	var response driftAuthorityAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("API returned success=false")
	}

	// Transform to DiscoverableAccount
	accounts := make([]*models.DiscoverableAccount, 0, len(response.Accounts))
	for _, acc := range response.Accounts {
		accountType := "main"
		if acc.SubAccountID > 0 {
			accountType = "sub_account"
		}

		name := acc.Name
		if name == "" {
			if acc.SubAccountID == 0 {
				name = "Main Account"
			} else {
				name = fmt.Sprintf("Subaccount %d", acc.SubAccountID)
			}
		}

		accounts = append(accounts, &models.DiscoverableAccount{
			AccountIdentifier: acc.AccountID,
			AccountType:       accountType,
			Name:              name,
			Metadata: map[string]interface{}{
				"authority":      userIdentifier,
				"sub_account_id": acc.SubAccountID,
			},
		})
	}

	return accounts, nil
}
