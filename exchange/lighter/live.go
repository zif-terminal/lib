package lighter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/zif-terminal/lib/models"
)

// Lighter account API response types for /api/v1/account

type lighterAccountResponse struct {
	Accounts []lighterFullAccount `json:"accounts"`
}

type lighterFullAccount struct {
	AccountID       int                   `json:"accountId"`
	TotalAssetValue string                `json:"total_asset_value"`
	Collateral      string                `json:"collateral"`
	AvailableBalance string               `json:"available_balance"`
	Positions       []lighterPositionData `json:"positions"`
	Assets          []lighterAssetData    `json:"assets"`
}

type lighterPositionData struct {
	Symbol           string      `json:"symbol"`
	MarketID         int         `json:"market_id"`
	Position         string      `json:"position"` // signed size
	AvgEntryPrice    string      `json:"avg_entry_price"`
	UnrealizedPnl    string      `json:"unrealized_pnl"`
	RealizedPnl      string      `json:"realized_pnl"`
	LiquidationPrice interface{} `json:"liquidation_price"`
	MarginMode       int         `json:"margin_mode"` // 0 = cross
	AllocatedMargin  string      `json:"allocated_margin"`
}

type lighterAssetData struct {
	Symbol        string `json:"symbol"`
	AssetID       int    `json:"asset_id"`
	Balance       string `json:"balance"`
	LockedBalance string `json:"locked_balance"`
}

// getL1Address extracts the L1 address from account metadata or identifier
func (c *Client) getL1Address(account *models.ExchangeAccount) (string, error) {
	// Try to get l1_address from metadata first
	if account.AccountTypeMetadata != nil {
		var metadata map[string]interface{}
		if err := json.Unmarshal(account.AccountTypeMetadata, &metadata); err == nil {
			if addr, ok := metadata["l1_address"].(string); ok && addr != "" {
				return addr, nil
			}
		}
	}

	// Fall back to wallet lookup if available
	return "", fmt.Errorf("l1_address not found in account metadata; required for live data fetching")
}

// fetchAccountData fetches account data from the Lighter API by L1 address
func (c *Client) fetchAccountData(ctx context.Context, l1Address string) (*lighterAccountResponse, error) {
	url := fmt.Sprintf("%s/api/v1/account?by=l1_address&value=%s", c.baseURL, l1Address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 404 or 400 means account not found — return empty
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return &lighterAccountResponse{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var result lighterAccountResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// FetchPositions fetches current open positions from Lighter
func (c *Client) FetchPositions(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.PositionSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	l1Address, err := c.getL1Address(account)
	if err != nil {
		return nil, err
	}

	data, err := c.fetchAccountData(ctx, l1Address)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account data: %w", err)
	}

	var positions []*models.PositionSnapshot
	for _, acct := range data.Accounts {
		for _, p := range acct.Positions {
			size := parseFloat(p.Position)
			if size == 0 {
				continue
			}

			liqPrice := 0.0
			if p.LiquidationPrice != nil {
				if s, ok := p.LiquidationPrice.(string); ok {
					liqPrice = parseFloat(s)
				} else if f, ok := p.LiquidationPrice.(float64); ok {
					liqPrice = f
				}
			}

			side := "long"
			if size < 0 {
				side = "short"
			}

			positions = append(positions, &models.PositionSnapshot{
				Symbol:           p.Symbol,
				Side:             side,
				Size:             size,
				EntryPrice:       parseFloat(p.AvgEntryPrice),
				LiquidationPrice: liqPrice,
				UnrealizedPnl:    parseFloat(p.UnrealizedPnl),
				MarketType:       "perp",
			})
		}
	}

	return positions, nil
}

// FetchBalances fetches current spot balances from Lighter
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	l1Address, err := c.getL1Address(account)
	if err != nil {
		return nil, err
	}

	data, err := c.fetchAccountData(ctx, l1Address)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account data: %w", err)
	}

	var balances []*models.BalanceSnapshot
	for _, acct := range data.Accounts {
		for _, a := range acct.Assets {
			bal := parseFloat(a.Balance)
			if bal == 0 {
				continue
			}
			balances = append(balances, &models.BalanceSnapshot{
				Asset:   a.Symbol,
				Balance: bal,
			})
		}
	}

	return balances, nil
}

// FetchOpenOrders returns empty orders for Lighter (requires authentication not available)
func (c *Client) FetchOpenOrders(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.OpenOrder, error) {
	// Open orders require API key authentication which is not available
	return []*models.OpenOrder{}, nil
}

// FetchAccountValue fetches total account value from Lighter
func (c *Client) FetchAccountValue(
	ctx context.Context,
	account *models.ExchangeAccount,
) (*models.AccountValueSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	l1Address, err := c.getL1Address(account)
	if err != nil {
		return nil, err
	}

	data, err := c.fetchAccountData(ctx, l1Address)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch account data: %w", err)
	}

	totalEquity := 0.0
	totalCollateral := 0.0
	totalAvailable := 0.0

	for _, acct := range data.Accounts {
		totalEquity += parseFloat(acct.TotalAssetValue)
		totalCollateral += parseFloat(acct.Collateral)
		totalAvailable += parseFloat(acct.AvailableBalance)
	}

	return &models.AccountValueSnapshot{
		TotalEquity:    totalEquity,
		FreeCollateral: totalAvailable,
		MarginUsed:     totalCollateral - totalAvailable,
	}, nil
}

// parseFloat converts a string to float64, returning 0 on error
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
