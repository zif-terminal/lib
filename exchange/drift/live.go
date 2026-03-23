package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/zif-terminal/lib/models"
)

// Drift /user/{accountId} response types

type driftUserResponse struct {
	Balances []driftUserBalance `json:"balances"`
}

type driftUserBalance struct {
	Symbol      string `json:"symbol"`
	MarketIndex int    `json:"marketIndex"`
	Balance     string `json:"balance"`
}

// Earn snapshots response types (used by FetchHistoricalBalanceSnapshots)

type driftEarnSnapshot struct {
	EpochTs int64            `json:"ts"`
	Assets  []driftEarnAsset `json:"assets"`
}

type driftEarnAsset struct {
	Symbol      string `json:"symbol"`
	MarketIndex int    `json:"marketIndex"`
	Balance     string `json:"balance"`
}

type driftEarnAccountSnapshot struct {
	AccountID string              `json:"accountId"`
	Snapshots []driftEarnSnapshot `json:"snapshots"`
}

type driftEarnResponse struct {
	Success  bool                       `json:"success"`
	Accounts []driftEarnAccountSnapshot `json:"accounts"`
}

// fetchUserAccount fetches per-account balance data
func (c *Client) fetchUserAccount(ctx context.Context, accountID string) (*driftUserResponse, error) {
	url := fmt.Sprintf("%s/user/%s", c.baseURL, accountID)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result driftUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// discoverAccountIDs discovers subaccount IDs for a wallet
func (c *Client) discoverAccountIDs(ctx context.Context, wallet string) ([]driftAuthorityAccount, error) {
	url := fmt.Sprintf("%s/authority/%s/accounts", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var result driftAuthorityAccountsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Accounts, nil
}

// getWalletFromAccount extracts the wallet address from the account
// For Drift, the AccountIdentifier is the subaccount pubkey, but we need the wallet authority
func (c *Client) getWalletFromAccount(account *models.ExchangeAccount) string {
	if account.AccountTypeMetadata != nil {
		var metadata map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &metadata); err == nil {
			if wallet, ok := metadata["authority"].(string); ok && wallet != "" {
				return wallet
			}
			if wallet, ok := metadata["wallet"].(string); ok && wallet != "" {
				return wallet
			}
		}
	}
	return ""
}

// FetchBalances fetches current spot balances from Drift.
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountID := account.AccountIdentifier
	if accountID == "" {
		return nil, fmt.Errorf("account identifier (subaccount public key) is required")
	}

	userData, err := c.fetchUserAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user data: %w", err)
	}
	if userData == nil {
		return []*models.BalanceSnapshot{}, nil
	}

	var balances []*models.BalanceSnapshot
	for _, b := range userData.Balances {
		balance := toFloat(b.Balance)
		if math.Abs(balance) < 0.000001 {
			continue
		}

		balances = append(balances, &models.BalanceSnapshot{
			Asset:   b.Symbol,
			Balance: balance,
		})
	}

	return balances, nil
}

// FetchHistoricalBalanceSnapshots returns all historical earn snapshots from Drift.
// Each entry represents a point-in-time snapshot of all asset balances.
// Returns snapshots sorted by timestamp ascending (oldest first).
func (c *Client) FetchHistoricalBalanceSnapshots(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.HistoricalBalanceSnapshots, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	wallet := c.getWalletFromAccount(account)
	if wallet == "" {
		return nil, fmt.Errorf("wallet address required for historical snapshots")
	}

	accountID := account.AccountIdentifier

	url := fmt.Sprintf("%s/authority/%s/snapshots/earn", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("earn snapshots returned status %d", resp.StatusCode)
	}

	var result driftEarnResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode earn response: %w", err)
	}

	if !result.Success {
		return nil, fmt.Errorf("earn snapshots returned success=false")
	}

	_ = c.fetchMarkets(ctx)

	for _, acct := range result.Accounts {
		if acct.AccountID != accountID {
			continue
		}

		var snapshots []*models.HistoricalBalanceSnapshots
		// Iterate in reverse so we return oldest-first
		for i := len(acct.Snapshots) - 1; i >= 0; i-- {
			snap := acct.Snapshots[i]
			var balances []*models.BalanceSnapshot
			for _, asset := range snap.Assets {
				balance := toFloat(asset.Balance)
				if math.Abs(balance) < 0.000001 {
					continue
				}

				symbol := asset.Symbol
				if symbol == "" {
					if info, ok := c.marketCache.getMarket(asset.MarketIndex, "spot"); ok {
						symbol = info.BaseAsset
					}
				}
				if symbol == "" {
					continue
				}

				balances = append(balances, &models.BalanceSnapshot{
					Asset:   symbol,
					Balance: balance,
				})
			}

			if len(balances) > 0 {
				snapshots = append(snapshots, &models.HistoricalBalanceSnapshots{
					TimestampMs: snap.EpochTs * 1000, // earn API returns epoch seconds
					Balances:    balances,
				})
			}
		}

		return snapshots, nil
	}

	return nil, nil
}

// toFloat converts various types to float64
func toFloat(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0
		}
		return f
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
