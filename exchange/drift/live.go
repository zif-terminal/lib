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

const dlobBaseURL = "https://dlob.drift.trade"

// Drift /user/{accountId} response types

type driftUserResponse struct {
	Positions []driftUserPosition `json:"positions"`
	Balances  []driftUserBalance  `json:"balances"`
	Orders    []driftUserOrder    `json:"orders"`
}

type driftUserPosition struct {
	Symbol           string `json:"symbol"`
	MarketIndex      int    `json:"marketIndex"`
	BaseAssetAmount  string `json:"baseAssetAmount"`
	QuoteEntryAmount string `json:"quoteEntryAmount"`
	LiquidationPrice string `json:"liquidationPrice"`
}

type driftUserBalance struct {
	Symbol           string `json:"symbol"`
	MarketIndex      int    `json:"marketIndex"`
	Balance          string `json:"balance"`
	LiquidationPrice string `json:"liquidationPrice"`
}

type driftUserOrder struct {
	Symbol          string `json:"symbol"`
	Direction       string `json:"direction"`
	Price           string `json:"price"`
	BaseAssetAmount string `json:"baseAssetAmount"`
	OrderType       string `json:"orderType"`
	ReduceOnly      bool   `json:"reduceOnly"`
	Status          string `json:"status"`
}

// DLOB /l2 response

type dlobL2Response struct {
	Oracle    interface{} `json:"oracle"`
	MarkPrice interface{} `json:"markPrice"`
}

// Snapshots response types

type driftSnapshotResponse struct {
	Success  bool                     `json:"success"`
	Accounts []driftSnapshotAccountV2 `json:"accounts"`
}

type driftSnapshotAccountV2 struct {
	AccountID string                  `json:"accountId"`
	Snapshots []driftOverviewSnapshot `json:"snapshots"`
}

type driftOverviewSnapshot struct {
	TotalAccountValue string `json:"totalAccountValue"`
	AccountBalance    string `json:"accountBalance"`
}

type driftEarnSnapshot struct {
	Assets []driftEarnAsset `json:"assets"`
}

type driftEarnAsset struct {
	MarketIndex int    `json:"marketIndex"`
	Balance     string `json:"balance"`
	OraclePrice string `json:"oraclePrice"`
}

type driftEarnAccountSnapshot struct {
	AccountID string              `json:"accountId"`
	Snapshots []driftEarnSnapshot `json:"snapshots"`
}

type driftEarnResponse struct {
	Success  bool                       `json:"success"`
	Accounts []driftEarnAccountSnapshot `json:"accounts"`
}

// stablecoins that should be treated as collateral, not positions
var stablecoins = map[string]bool{
	"USDC": true, "USDT": true, "DAI": true, "BUSD": true,
}

// fetchDLOBPrice fetches oracle/mark price for a perp market from DLOB
func (c *Client) fetchDLOBPrice(ctx context.Context, marketIndex int) (oracle, mark float64) {
	url := fmt.Sprintf("%s/l2?marketIndex=%d&marketType=perp&depth=1", dlobBaseURL, marketIndex)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}

	var result dlobL2Response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, 0
	}

	oracle = toFloat(result.Oracle) / float64(QuotePrecision)
	mark = toFloat(result.MarkPrice) / float64(QuotePrecision)
	return oracle, mark
}

// fetchUserAccount fetches per-account position/balance/order data
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

// fetchEarnSnapshots fetches spot oracle prices from earn snapshots
func (c *Client) fetchEarnSnapshots(ctx context.Context, wallet string) (map[int]float64, error) {
	url := fmt.Sprintf("%s/authority/%s/snapshots/earn", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[int]float64{}, nil
	}

	var result driftEarnResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return map[int]float64{}, nil
	}

	prices := make(map[int]float64)
	for _, acct := range result.Accounts {
		if len(acct.Snapshots) == 0 {
			continue
		}
		latest := acct.Snapshots[0]
		for _, asset := range latest.Assets {
			price := toFloat(asset.OraclePrice)
			if price > 0 {
				prices[asset.MarketIndex] = price
			}
		}
	}

	return prices, nil
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
	// Try to get wallet from metadata
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

// FetchPositions fetches current open positions from Drift
func (c *Client) FetchPositions(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.PositionSnapshot, error) {
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
		return []*models.PositionSnapshot{}, nil
	}

	// Collect unique perp market indices for DLOB price fetching
	perpPrices := make(map[int][2]float64) // marketIndex -> [oracle, mark]
	for _, p := range userData.Positions {
		if _, exists := perpPrices[p.MarketIndex]; !exists {
			oracle, mark := c.fetchDLOBPrice(ctx, p.MarketIndex)
			perpPrices[p.MarketIndex] = [2]float64{oracle, mark}
		}
	}

	// Get spot oracle prices
	wallet := c.getWalletFromAccount(account)
	spotOraclePrices := map[int]float64{}
	if wallet != "" {
		spotOraclePrices, _ = c.fetchEarnSnapshots(ctx, wallet)
	}

	var positions []*models.PositionSnapshot

	// Parse perp positions
	for _, p := range userData.Positions {
		baseAmount := toFloat(p.BaseAssetAmount)
		if baseAmount == 0 {
			continue
		}

		quoteEntry := toFloat(p.QuoteEntryAmount)
		entryPrice := 0.0
		if baseAmount != 0 {
			entryPrice = math.Abs(quoteEntry / baseAmount)
		}

		prices := perpPrices[p.MarketIndex]
		markPrice := prices[0] // oracle
		if markPrice == 0 {
			markPrice = prices[1] // mark
		}
		if markPrice == 0 {
			markPrice = entryPrice
		}

		liqPrice := toFloat(p.LiquidationPrice)
		side := "long"
		if baseAmount < 0 {
			side = "short"
		}

		positions = append(positions, &models.PositionSnapshot{
			Symbol:           p.Symbol,
			Side:             side,
			Size:             baseAmount,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			LiquidationPrice: liqPrice,
			UnrealizedPnl:    (markPrice - entryPrice) * baseAmount,
			MarketType:       "perp",
		})
	}

	// Parse non-stablecoin spot balances as positions
	for _, b := range userData.Balances {
		balance := toFloat(b.Balance)
		if math.Abs(balance) < 0.000001 {
			continue
		}
		if stablecoins[b.Symbol] {
			continue
		}

		oraclePrice := spotOraclePrices[b.MarketIndex]
		liqPrice := toFloat(b.LiquidationPrice)

		side := "long"
		if balance < 0 {
			side = "short"
		}

		positions = append(positions, &models.PositionSnapshot{
			Symbol:           b.Symbol,
			Side:             side,
			Size:             balance,
			EntryPrice:       oraclePrice,
			MarkPrice:        oraclePrice,
			LiquidationPrice: liqPrice,
			MarketType:       "spot",
		})
	}

	return positions, nil
}

// FetchBalances fetches current spot balances from Drift
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

	// Get spot oracle prices from earn snapshots
	wallet := c.getWalletFromAccount(account)
	spotOraclePrices := map[int]float64{}
	if wallet != "" {
		spotOraclePrices, _ = c.fetchEarnSnapshots(ctx, wallet)
	}

	var balances []*models.BalanceSnapshot
	for _, b := range userData.Balances {
		balance := toFloat(b.Balance)
		if math.Abs(balance) < 0.000001 {
			continue
		}

		oraclePrice := spotOraclePrices[b.MarketIndex]

		balances = append(balances, &models.BalanceSnapshot{
			Asset:       b.Symbol,
			Balance:     balance,
			UsdValue:    balance * oraclePrice,
			OraclePrice: oraclePrice,
		})
	}

	return balances, nil
}

// FetchOpenOrders fetches currently active orders from Drift
func (c *Client) FetchOpenOrders(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.OpenOrder, error) {
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
		return []*models.OpenOrder{}, nil
	}

	var orders []*models.OpenOrder
	for _, o := range userData.Orders {
		if o.Status != "open" {
			continue
		}

		orders = append(orders, &models.OpenOrder{
			Symbol:     o.Symbol,
			Side:       o.Direction,
			Size:       toFloat(o.BaseAssetAmount),
			Price:      toFloat(o.Price),
			OrderType:  o.OrderType,
			ReduceOnly: o.ReduceOnly,
		})
	}

	return orders, nil
}

// FetchAccountValue fetches total account value from Drift
func (c *Client) FetchAccountValue(
	ctx context.Context,
	account *models.ExchangeAccount,
) (*models.AccountValueSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	wallet := c.getWalletFromAccount(account)
	if wallet == "" {
		// Fall back: just use the account identifier and try direct user query
		return &models.AccountValueSnapshot{}, nil
	}

	// Try overview snapshot first
	url := fmt.Sprintf("%s/authority/%s/snapshots/overview", c.baseURL, wallet)
	resp, err := c.doRequestWithRetry(ctx, url)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var result driftSnapshotResponse
			if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Success {
				totalValue := 0.0
				for _, acct := range result.Accounts {
					if len(acct.Snapshots) > 0 {
						latest := acct.Snapshots[0]
						v := toFloat(latest.TotalAccountValue)
						if v == 0 {
							v = toFloat(latest.AccountBalance)
						}
						totalValue += v
					}
				}
				if totalValue > 0 {
					return &models.AccountValueSnapshot{
						TotalEquity: totalValue,
					}, nil
				}
			}
		}
	}

	return &models.AccountValueSnapshot{}, nil
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
