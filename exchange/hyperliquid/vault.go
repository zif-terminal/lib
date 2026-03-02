package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// FetchVaults returns all public Hyperliquid vaults.
// Uses POST /info with {"type": "vaults"}.
func (c *Client) FetchVaults(ctx context.Context) ([]*VaultSummary, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	reqBody := map[string]interface{}{"type": "vaults"}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaults: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaults: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaults: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid FetchVaults: status %d", resp.StatusCode)
	}

	var raw []hyperliquidVaultSummary
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaults: decode: %w", err)
	}

	out := make([]*VaultSummary, 0, len(raw))
	for _, v := range raw {
		out = append(out, &VaultSummary{
			Address:     v.Vault,
			Name:        v.Name,
			Leader:      v.Leader,
			Description: v.Description,
			TVL:         v.Tvl,
			APR:         v.Apr,
			IsClosed:    v.IsClosed,
		})
	}
	return out, nil
}

// FetchVaultDetails returns details for a single vault.
// Uses POST /info with {"type": "vaultDetails", "vaultAddress": "0x..."}.
func (c *Client) FetchVaultDetails(ctx context.Context, vaultAddress string) (*VaultSummary, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	reqBody := map[string]interface{}{
		"type":         "vaultDetails",
		"vaultAddress": vaultAddress,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: vault %s not found", vaultAddress)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: status %d", resp.StatusCode)
	}

	var raw hyperliquidVaultDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("hyperliquid FetchVaultDetails: decode: %w", err)
	}

	return &VaultSummary{
		Address:     raw.Vault,
		Name:        raw.Name,
		Leader:      raw.Leader,
		Description: raw.Description,
		TVL:         raw.Tvl,
		APR:         raw.Apr,
		IsClosed:    raw.IsClosed,
	}, nil
}

// FetchUserVaultEquities returns the user's equity across all vaults they have deposited in.
// Uses POST /info with {"type": "userVaultEquities", "user": "0x..."}.
func (c *Client) FetchUserVaultEquities(ctx context.Context, userAddress string) ([]*UserVaultEquity, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	reqBody := map[string]interface{}{
		"type": "userVaultEquities",
		"user": userAddress,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchUserVaultEquities: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchUserVaultEquities: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid FetchUserVaultEquities: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyperliquid FetchUserVaultEquities: status %d", resp.StatusCode)
	}

	var raw []hyperliquidUserVaultEquity
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("hyperliquid FetchUserVaultEquities: decode: %w", err)
	}

	out := make([]*UserVaultEquity, 0, len(raw))
	for _, e := range raw {
		out = append(out, &UserVaultEquity{
			VaultAddress: e.VaultAddress,
			Equity:       e.Equity,
		})
	}
	return out, nil
}
