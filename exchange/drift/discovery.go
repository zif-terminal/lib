package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zif-terminal/lib/models"
)

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

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch accounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	if resp.StatusCode != http.StatusOK {
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
				"authority":     userIdentifier,
				"sub_account_id": acc.SubAccountID,
			},
		})
	}

	return accounts, nil
}
