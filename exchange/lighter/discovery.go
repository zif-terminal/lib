package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zif-terminal/lib/models"
)

// DiscoverAccounts discovers all accounts associated with a user identifier.
// For Lighter, the userIdentifier format is: "{l1_address}:{auth_token}"
// where auth_token follows the format: "ro:{account_index}:{single|all}:{expiry_unix}:{random_hex}"
func (c *Client) DiscoverAccounts(
	ctx context.Context,
	userIdentifier string,
) ([]*models.DiscoverableAccount, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse userIdentifier: "0x...l1_address:ro:account_index:..."
	// Split on first colon to get l1_address, everything after is the auth token
	parts := strings.SplitN(userIdentifier, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid userIdentifier format: expected '{l1_address}:{auth_token}'")
	}

	l1Address := parts[0]
	authToken := parts[1]

	if l1Address == "" {
		return nil, fmt.Errorf("l1_address is required")
	}

	if !strings.HasPrefix(authToken, "ro:") {
		return nil, fmt.Errorf("invalid auth token format: must start with 'ro:'")
	}

	// Call API to discover accounts
	accounts, err := c.fetchAccountsByL1Address(ctx, l1Address, authToken)
	if err != nil {
		return nil, err
	}

	result := make([]*models.DiscoverableAccount, 0, len(accounts))

	for _, acc := range accounts {
		// AccountType: 0 = main account, 1 = sub-account
		accountType := "sub_account"
		name := fmt.Sprintf("Sub-Account %d", acc.Index)
		if acc.AccountType == 0 {
			accountType = "main"
			name = "Main Account"
		}

		result = append(result, &models.DiscoverableAccount{
			AccountIdentifier: strconv.Itoa(acc.Index),
			AccountType:       accountType,
			Name:              name,
			Metadata: map[string]interface{}{
				"l1_address": l1Address,
				"auth_token": authToken,
			},
		})
	}

	return result, nil
}

// fetchAccountsByL1Address fetches accounts from the API
func (c *Client) fetchAccountsByL1Address(
	ctx context.Context,
	l1Address string,
	authToken string,
) ([]lighterAccount, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accountsByL1Address", c.baseURL)

	params := url.Values{}
	params.Set("l1_address", l1Address)

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req, authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.handleResponseError(resp); err != nil {
		return nil, err
	}

	var result lighterAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.SubAccounts, nil
}
