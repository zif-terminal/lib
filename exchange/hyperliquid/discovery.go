package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/zif-terminal/lib/models"
)

// DiscoverAccounts discovers Hyperliquid accounts for a given Ethereum wallet address.
// Checks if the address has any perp positions or spot balances indicating activity.
// Also discovers subaccounts and vaults where the user is the leader.
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

	// Fallback: check historical ledger activity for wallets that traded but withdrew all funds
	if !hasActivity {
		req := map[string]interface{}{
			"type": "userNonFundingLedgerUpdates",
			"user": userIdentifier,
		}
		var ledgerEntries []interface{}
		if err := c.doPost(ctx, req, &ledgerEntries); err == nil && len(ledgerEntries) > 0 {
			hasActivity = true
		}
	}

	if !hasActivity {
		return nil, nil
	}

	accounts := []*models.DiscoverableAccount{
		{
			AccountIdentifier: userIdentifier,
			AccountType:       "main",
			Name:              "Main Account",
			Metadata: map[string]interface{}{
				"wallet": userIdentifier,
			},
		},
	}

	// Discover subaccounts
	subAccounts, err := c.discoverSubAccounts(ctx, userIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to discover subaccounts: %w", err)
	}
	accounts = append(accounts, subAccounts...)

	// Discover vaults where user is the leader
	vaultAccounts, err := c.discoverLeaderVaults(ctx, userIdentifier)
	if err != nil {
		return nil, fmt.Errorf("failed to discover vaults: %w", err)
	}
	accounts = append(accounts, vaultAccounts...)

	return accounts, nil
}

// discoverSubAccounts fetches subaccounts for the given wallet address.
// Returns an empty slice if the user has no subaccounts.
func (c *Client) discoverSubAccounts(ctx context.Context, walletAddress string) ([]*models.DiscoverableAccount, error) {
	// The API returns null for no subaccounts, so we use json.RawMessage to handle both cases.
	var raw json.RawMessage
	err := c.doPost(ctx, map[string]string{
		"type": "subAccounts",
		"user": walletAddress,
	}, &raw)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch subaccounts: %w", err)
	}

	// null response means no subaccounts
	if raw == nil || string(raw) == "null" {
		return nil, nil
	}

	var subAccounts []hlSubAccount
	if err := json.Unmarshal(raw, &subAccounts); err != nil {
		return nil, fmt.Errorf("failed to parse subaccounts response: %w", err)
	}

	var accounts []*models.DiscoverableAccount
	for i, sub := range subAccounts {
		name := sub.Name
		if name == "" {
			name = fmt.Sprintf("Subaccount %d", i+1)
		}
		accounts = append(accounts, &models.DiscoverableAccount{
			AccountIdentifier: sub.SubAccountUser,
			AccountType:       "sub_account",
			Name:              name,
			Metadata: map[string]interface{}{
				"master_wallet":   sub.Master,
				"subaccount_name": sub.Name,
			},
		})
	}

	return accounts, nil
}

// discoverLeaderVaults discovers vaults where the user is the leader.
// Passive depositors are skipped — their cashflows are already tracked via ledger entries.
func (c *Client) discoverLeaderVaults(ctx context.Context, walletAddress string) ([]*models.DiscoverableAccount, error) {
	// Fetch vault equities for this user
	var raw json.RawMessage
	err := c.doPost(ctx, map[string]string{
		"type": "userVaultEquities",
		"user": walletAddress,
	}, &raw)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user vault equities: %w", err)
	}

	// null or empty response means no vault participation
	if raw == nil || string(raw) == "null" || string(raw) == "[]" {
		return nil, nil
	}

	var vaultEquities []hlVaultEquity
	if err := json.Unmarshal(raw, &vaultEquities); err != nil {
		return nil, fmt.Errorf("failed to parse vault equities response: %w", err)
	}

	var accounts []*models.DiscoverableAccount
	for _, ve := range vaultEquities {
		if ve.VaultAddress == "" {
			continue
		}

		// Fetch vault details to determine leader
		details, err := c.fetchVaultDetails(ctx, ve.VaultAddress)
		if err != nil {
			// Skip vaults we can't fetch details for (may be deprecated/deleted)
			continue
		}

		// Only add vaults where the user is the leader
		if !strings.EqualFold(details.Leader, walletAddress) {
			continue
		}

		name := details.Name
		if name == "" {
			name = "Vault " + ve.VaultAddress[:8]
		}

		accounts = append(accounts, &models.DiscoverableAccount{
			AccountIdentifier: ve.VaultAddress,
			AccountType:       "vault",
			Name:              name,
			Metadata: map[string]interface{}{
				"vault_name": details.Name,
				"is_leader":  true,
			},
		})
	}

	return accounts, nil
}

// FetchAccountName returns the exchange-assigned subaccount name from the
// Hyperliquid subAccounts endpoint. Master/main accounts and vaults return "" —
// Hyperliquid does not surface names for them via this API.
//
// Looks up the master wallet from account.AccountTypeMetadata["master_wallet"]
// (populated by DiscoverAccounts), calls POST /info {type:"subAccounts",user:master},
// and returns the name of the entry whose subAccountUser matches account.AccountIdentifier
// (case-insensitive). Returns "" if the subaccount was removed upstream.
func (c *Client) FetchAccountName(ctx context.Context, account *models.ExchangeAccount) (string, error) {
	if account == nil {
		return "", fmt.Errorf("account is required")
	}

	// Only subaccounts have names surfaced via the API.
	if account.AccountType != "sub_account" {
		return "", nil
	}

	// Extract master wallet address from metadata.
	master := ""
	if account.AccountTypeMetadata != nil {
		var meta map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &meta); err == nil {
			if v, ok := meta["master_wallet"].(string); ok {
				master = v
			}
		}
	}
	if master == "" {
		return "", fmt.Errorf("master_wallet missing from account metadata")
	}

	// The API returns null when the master has no subaccounts.
	var raw json.RawMessage
	if err := c.doPost(ctx, map[string]string{
		"type": "subAccounts",
		"user": master,
	}, &raw); err != nil {
		return "", fmt.Errorf("failed to fetch subaccounts: %w", err)
	}
	if raw == nil || string(raw) == "null" {
		return "", nil
	}

	var subs []hlSubAccount
	if err := json.Unmarshal(raw, &subs); err != nil {
		return "", fmt.Errorf("failed to parse subaccounts response: %w", err)
	}

	for _, s := range subs {
		if strings.EqualFold(s.SubAccountUser, account.AccountIdentifier) {
			return s.Name, nil
		}
	}

	// Subaccount was removed upstream — no match is not an error.
	return "", nil
}

// fetchVaultDetails fetches the details of a specific vault.
func (c *Client) fetchVaultDetails(ctx context.Context, vaultAddress string) (*hlVaultDetails, error) {
	// The vaultDetails response wraps vault info in various fields.
	// We only need name and leader, which are at the top level.
	var raw json.RawMessage
	err := c.doPost(ctx, map[string]string{
		"type":         "vaultDetails",
		"vaultAddress": vaultAddress,
	}, &raw)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch vault details for %s: %w", vaultAddress, err)
	}

	if raw == nil || string(raw) == "null" {
		return nil, fmt.Errorf("vault %s not found", vaultAddress)
	}

	var details hlVaultDetails
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, fmt.Errorf("failed to parse vault details for %s: %w", vaultAddress, err)
	}

	return &details, nil
}
