package drift

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zif-terminal/lib/models"
)

// FetchAccountName returns the user-set Drift subaccount name (e.g.
// "Funding", "Spot", "USDC Savings") by querying the authority's account list.
//
// Drift's public `GET /authority/<wallet>/accounts` endpoint returns each
// subaccount's `name` field. The authority address is stored in
// `account_type_metadata.authority` at discovery time. Returns "" with no
// error when authority metadata is missing or the account doesn't appear in
// the response — caller's guard skips empty labels.
func (c *Client) FetchAccountName(ctx context.Context, account *models.ExchangeAccount) (string, error) {
	if account == nil || account.AccountTypeMetadata == nil {
		return "", nil
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(account.AccountTypeMetadata, &meta); err != nil {
		return "", nil
	}
	authority, _ := meta["authority"].(string)
	if authority == "" {
		return "", nil
	}

	c.principal = authority
	defer func() { c.principal = "" }()

	url := fmt.Sprintf("%s/authority/%s/accounts", c.baseURL, authority)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch authority accounts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("authority accounts API returned status %d", resp.StatusCode)
	}

	var response driftAuthorityAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode authority accounts response: %w", err)
	}
	if !response.Success {
		return "", nil
	}
	for _, acc := range response.Accounts {
		if acc.AccountID == account.AccountIdentifier {
			return acc.Name, nil
		}
	}
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

	c.principal = userIdentifier
	defer func() { c.principal = "" }()

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
