package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zif-terminal/lib/models"
)

// subAccountResponse represents the response from subAccounts API
type subAccountResponse struct {
	SubAccountUser string `json:"subAccountUser"` // The subaccount address
	Name           string `json:"name"`           // The subaccount name
	Master         string `json:"master"`         // The master account address
}

// vaultEquityResponse represents the response from userVaultEquities API
type vaultEquityResponse struct {
	Vault       string `json:"vault"`       // The vault address
	VaultName   string `json:"vaultName"`   // The vault name
	Equity      string `json:"equity"`      // User's equity in the vault
	VaultEquity string `json:"vaultEquity"` // Total vault equity
}

// DiscoverAccounts discovers all syncable accounts for a given wallet address
// This includes: main account, subaccounts, and vaults the user has deposited in
func (c *Client) DiscoverAccounts(ctx context.Context, userIdentifier string) ([]*models.DiscoverableAccount, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if userIdentifier == "" {
		return nil, fmt.Errorf("user identifier (wallet address) is required")
	}

	accounts := make([]*models.DiscoverableAccount, 0)

	// 1. Add main account
	accounts = append(accounts, &models.DiscoverableAccount{
		AccountIdentifier: userIdentifier,
		AccountType:       "main",
		Name:              "Main Account",
		Metadata:          map[string]interface{}{},
	})

	// 2. Fetch subaccounts
	subAccounts, err := c.fetchSubAccounts(ctx, userIdentifier)
	if err != nil {
		// Log but don't fail - we still have the main account
		// In production, consider proper logging
	} else {
		for _, sub := range subAccounts {
			name := sub.Name
			if name == "" {
				name = fmt.Sprintf("Subaccount %s", truncateAddress(sub.SubAccountUser))
			}
			accounts = append(accounts, &models.DiscoverableAccount{
				AccountIdentifier: sub.SubAccountUser,
				AccountType:       "sub_account",
				Name:              name,
				Metadata: map[string]interface{}{
					"master": userIdentifier,
				},
			})
		}
	}

	// 3. Fetch vaults user has deposited in
	vaults, err := c.fetchUserVaultEquities(ctx, userIdentifier)
	if err != nil {
		// Log but don't fail
	} else {
		for _, vault := range vaults {
			name := vault.VaultName
			if name == "" {
				name = fmt.Sprintf("Vault %s", truncateAddress(vault.Vault))
			}
			accounts = append(accounts, &models.DiscoverableAccount{
				AccountIdentifier: vault.Vault,
				AccountType:       "vault",
				Name:              name,
				Metadata: map[string]interface{}{
					"equity":       vault.Equity,
					"vault_equity": vault.VaultEquity,
				},
			})
		}
	}

	return accounts, nil
}

// fetchSubAccounts fetches subaccounts for a user
// POST /info {"type": "subAccounts", "user": "<address>"}
func (c *Client) fetchSubAccounts(ctx context.Context, userAddress string) ([]subAccountResponse, error) {
	requestBody := map[string]interface{}{
		"type": "subAccounts",
		"user": userAddress,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.createRequest(ctx, bodyBytes)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subaccounts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var subAccounts []subAccountResponse
	if err := json.NewDecoder(resp.Body).Decode(&subAccounts); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return subAccounts, nil
}

// fetchUserVaultEquities fetches vaults the user has deposited in
// POST /info {"type": "userVaultEquities", "user": "<address>"}
func (c *Client) fetchUserVaultEquities(ctx context.Context, userAddress string) ([]vaultEquityResponse, error) {
	requestBody := map[string]interface{}{
		"type": "userVaultEquities",
		"user": userAddress,
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := c.createRequest(ctx, bodyBytes)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch vault equities: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var vaults []vaultEquityResponse
	if err := json.NewDecoder(resp.Body).Decode(&vaults); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return vaults, nil
}

// createRequest creates an HTTP request for the Hyperliquid API
func (c *Client) createRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// truncateAddress returns a short version of an address for display
func truncateAddress(addr string) string {
	if len(addr) <= 10 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}
