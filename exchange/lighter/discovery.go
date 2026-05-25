package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/zif-terminal/lib/models"
)

// lighterNamedAccount is a slim decode of a Lighter account response that
// includes the optional `name` field. The main lighterAccount type does not
// declare it because it's only surfaced for label lookup.
type lighterNamedAccount struct {
	AccountIndex int    `json:"account_index"`
	Name         string `json:"name"`
}

type lighterNamedAccountResp struct {
	Code     int                   `json:"code"`
	Accounts []lighterNamedAccount `json:"accounts"`
}

// FetchAccountName returns the exchange-assigned name for a Lighter account,
// or "" if no name is set.
//
// Endpoint: GET /api/v1/account?by=index&value=<account_index>.
// The account_index is account.AccountIdentifier (populated by DiscoverAccounts).
// Lighter typically returns an empty name for newly-created accounts; callers
// should treat "" as "no name" and leave the label unset.
func (c *Client) FetchAccountName(ctx context.Context, account *models.ExchangeAccount) (string, error) {
	if account == nil {
		return "", fmt.Errorf("account is required")
	}
	if account.AccountIdentifier == "" {
		return "", fmt.Errorf("account_identifier (account_index) is required")
	}

	// Validate it's a numeric account_index before hitting the API.
	if _, err := strconv.Atoi(account.AccountIdentifier); err != nil {
		return "", fmt.Errorf("invalid account_index %q: %w", account.AccountIdentifier, err)
	}

	c.principal = principalFor(account)
	defer func() { c.principal = "" }()

	url := fmt.Sprintf("%s/account?by=index&value=%s", c.baseURL, account.AccountIdentifier)
	body, err := c.doGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("failed to fetch account: %w", err)
	}
	if body == nil {
		return "", nil
	}

	var resp lighterNamedAccountResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to decode account response: %w", err)
	}

	// Code 200 = success. Anything else (including 21100 "not found") → "".
	if resp.Code != 200 || len(resp.Accounts) == 0 {
		return "", nil
	}

	return resp.Accounts[0].Name, nil
}

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

	c.principal = userIdentifier
	defer func() { c.principal = "" }()

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
