package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

// perpDexes lists the perp dex variants to query (default + xyz)
var perpDexes = []string{"", "xyz"}

// clearinghouseState response types

type clearinghouseStateResponse struct {
	AssetPositions []assetPosition `json:"assetPositions"`
	MarginSummary  *marginSummary  `json:"marginSummary"`
}

type assetPosition struct {
	Position positionData `json:"position"`
}

type positionData struct {
	Coin           string      `json:"coin"`
	Szi            string      `json:"szi"`
	EntryPx        string      `json:"entryPx"`
	PositionValue  string      `json:"positionValue"`
	UnrealizedPnl  string      `json:"unrealizedPnl"`
	Leverage       *leverageV  `json:"leverage"`
	LiquidationPx  interface{} `json:"liquidationPx"`
	MarginUsed     string      `json:"marginUsed"`
}

type leverageV struct {
	Value string `json:"value"`
}

type marginSummary struct {
	AccountValue    string `json:"accountValue"`
	TotalMarginUsed string `json:"totalMarginUsed"`
}

// spotClearinghouseState response types

type spotClearinghouseStateResponse struct {
	Balances []spotBalance `json:"balances"`
}

type spotBalance struct {
	Coin  string `json:"coin"`
	Total string `json:"total"`
	Hold  string `json:"hold"`
}

// openOrders response types

type openOrderEntry struct {
	Coin      string `json:"coin"`
	Side      string `json:"side"`
	Sz        string `json:"sz"`
	LimitPx   string `json:"limitPx"`
	OrderType string `json:"orderType"`
	Timestamp int64  `json:"timestamp"`
}

// postInfo sends a POST request to /info and decodes the response
func (c *Client) postInfo(ctx context.Context, body map[string]interface{}, result interface{}) error {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/info", strings.NewReader(string(bodyBytes)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return &iface.RateLimitError{
			Exchange:   "hyperliquid",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	// 404 or 400 with "not found" means account doesn't exist
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}

// FetchPositions fetches current open perp + spot positions from Hyperliquid
func (c *Client) FetchPositions(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.PositionSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	address := account.AccountIdentifier
	if address == "" {
		return nil, fmt.Errorf("account identifier (address) is required")
	}

	var allPositions []*models.PositionSnapshot

	// Query clearinghouseState for each perp dex
	for _, dex := range perpDexes {
		body := map[string]interface{}{
			"type": "clearinghouseState",
			"user": address,
		}
		if dex != "" {
			body["dex"] = dex
		}

		var state clearinghouseStateResponse
		if err := c.postInfo(ctx, body, &state); err != nil {
			continue // Skip failed dex queries
		}

		for _, ap := range state.AssetPositions {
			p := ap.Position
			szi := parseFloat64(p.Szi)
			if szi == 0 {
				continue
			}

			positionValue := parseFloat64(p.PositionValue)
			markPrice := 0.0
			if math.Abs(szi) > 0 {
				markPrice = positionValue / math.Abs(szi)
			}

			leverage := 0.0
			if p.Leverage != nil {
				leverage = parseFloat64(p.Leverage.Value)
			}

			liqPrice := 0.0
			if p.LiquidationPx != nil {
				liqPrice = parseFloat64(convertToString(p.LiquidationPx))
			}

			side := "long"
			if szi < 0 {
				side = "short"
			}

			allPositions = append(allPositions, &models.PositionSnapshot{
				Symbol:           p.Coin,
				Side:             side,
				Size:             szi,
				EntryPrice:       parseFloat64(p.EntryPx),
				MarkPrice:        markPrice,
				LiquidationPrice: liqPrice,
				UnrealizedPnl:    parseFloat64(p.UnrealizedPnl),
				Leverage:         leverage,
				MarketType:       "perp",
			})
		}
	}

	return allPositions, nil
}

// FetchBalances fetches current spot balances from Hyperliquid
func (c *Client) FetchBalances(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.BalanceSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	address := account.AccountIdentifier
	if address == "" {
		return nil, fmt.Errorf("account identifier (address) is required")
	}

	var state spotClearinghouseStateResponse
	err := c.postInfo(ctx, map[string]interface{}{
		"type": "spotClearinghouseState",
		"user": address,
	}, &state)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spot balances: %w", err)
	}

	var balances []*models.BalanceSnapshot
	for _, b := range state.Balances {
		bal := parseFloat64(b.Total)
		if bal == 0 {
			continue
		}
		balances = append(balances, &models.BalanceSnapshot{
			Asset:   b.Coin,
			Balance: bal,
		})
	}

	return balances, nil
}

// FetchOpenOrders fetches currently active orders from Hyperliquid
func (c *Client) FetchOpenOrders(
	ctx context.Context,
	account *models.ExchangeAccount,
) ([]*models.OpenOrder, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	address := account.AccountIdentifier
	if address == "" {
		return nil, fmt.Errorf("account identifier (address) is required")
	}

	var apiOrders []openOrderEntry
	err := c.postInfo(ctx, map[string]interface{}{
		"type": "openOrders",
		"user": address,
	}, &apiOrders)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch open orders: %w", err)
	}

	var orders []*models.OpenOrder
	for _, o := range apiOrders {
		side := "buy"
		if o.Side == "S" || o.Side == "sell" {
			side = "sell"
		}

		orders = append(orders, &models.OpenOrder{
			Symbol:    o.Coin,
			Side:      side,
			Size:      parseFloat64(o.Sz),
			Price:     parseFloat64(o.LimitPx),
			OrderType: o.OrderType,
		})
	}

	return orders, nil
}

// FetchAccountValue fetches total equity/account value from Hyperliquid
func (c *Client) FetchAccountValue(
	ctx context.Context,
	account *models.ExchangeAccount,
) (*models.AccountValueSnapshot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	address := account.AccountIdentifier
	if address == "" {
		return nil, fmt.Errorf("account identifier (address) is required")
	}

	totalEquity := 0.0
	totalMarginUsed := 0.0

	// Sum account values from all perp dexes
	for _, dex := range perpDexes {
		body := map[string]interface{}{
			"type": "clearinghouseState",
			"user": address,
		}
		if dex != "" {
			body["dex"] = dex
		}

		var state clearinghouseStateResponse
		if err := c.postInfo(ctx, body, &state); err != nil {
			continue
		}

		if state.MarginSummary != nil {
			totalEquity += parseFloat64(state.MarginSummary.AccountValue)
			totalMarginUsed += parseFloat64(state.MarginSummary.TotalMarginUsed)
		}
	}

	return &models.AccountValueSnapshot{
		TotalEquity:    totalEquity,
		FreeCollateral: totalEquity - totalMarginUsed,
		MarginUsed:     totalMarginUsed,
	}, nil
}

// parseFloat64 converts a string to float64, returning 0 on error
func parseFloat64(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
