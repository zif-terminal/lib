package lighter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zif-terminal/lib/exchange/iface"
	"github.com/zif-terminal/lib/models"
)

const (
	defaultBaseURL = "https://mainnet.zklighter.elliot.ai"
	httpTimeout    = 30 * time.Second
	// Delay between pagination requests to avoid rate limits (60 req/min = 1 req/sec)
	paginationDelay = 1500 * time.Millisecond
	// Max retries for rate limit errors
	maxRateLimitRetries = 3
)

// marketInfo holds cached market data
type marketInfo struct {
	symbol     string
	baseAsset  string
	quoteAsset string
	marketType string // "perp" or "spot"
}

// marketCache provides thread-safe caching of market information
type marketCache struct {
	mu      sync.RWMutex
	markets map[int]marketInfo
	loaded  bool
}

// Client implements the ExchangeClient interface for Lighter.xyz
type Client struct {
	baseURL     string
	httpClient  *http.Client
	marketCache *marketCache
}

// NewClient creates a new Lighter client
func NewClient() *Client {
	return &Client{
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: httpTimeout},
		marketCache: &marketCache{
			markets: make(map[int]marketInfo),
		},
	}
}

// Name returns the exchange identifier
func (c *Client) Name() string {
	return "lighter"
}

// FetchTrades retrieves trades for an account since the given timestamp
func (c *Client) FetchTrades(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.TradeInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	authToken, err := c.getAuthToken(account)
	if err != nil {
		return nil, err
	}

	accountIndex := account.AccountIdentifier
	if accountIndex == "" {
		return nil, fmt.Errorf("account identifier (account_index) is required")
	}

	// Ensure market cache is loaded
	if err := c.loadMarketCache(ctx, authToken); err != nil {
		return nil, fmt.Errorf("failed to load market info: %w", err)
	}

	var allTrades []*models.TradeInput
	cursor := ""
	pageCount := 0

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Add delay between pagination requests to avoid rate limits
		if pageCount > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(paginationDelay):
			}
		}

		var trades []lighterTrade
		var nextCursor string
		var err error

		// Retry logic for rate limits
		for retry := 0; retry <= maxRateLimitRetries; retry++ {
			trades, nextCursor, err = c.fetchTrades(ctx, accountIndex, authToken, cursor)
			if err == nil {
				break
			}

			// Check if it's a rate limit error
			var rateLimitErr *iface.RateLimitError
			if errors.As(err, &rateLimitErr) && retry < maxRateLimitRetries {
				waitTime := rateLimitErr.RetryAfter
				if waitTime < time.Second {
					waitTime = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(waitTime):
				}
				continue
			}
			return nil, err
		}

		pageCount++

		for _, trade := range trades {
			// Timestamp is in Unix milliseconds
			ts := time.Unix(0, trade.Timestamp*int64(time.Millisecond)).UTC()
			if !since.IsZero() && ts.Before(since) {
				continue
			}

			tradeInput, err := c.tradeToTradeInput(trade, accountIndex, accountUUID)
			if err != nil {
				continue // Skip trades that can't be converted
			}

			allTrades = append(allTrades, tradeInput)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Sort by timestamp (oldest first)
	sort.Slice(allTrades, func(i, j int) bool {
		return allTrades[i].Timestamp.Before(allTrades[j].Timestamp)
	})

	return allTrades, nil
}

// FetchFundingPayments retrieves funding payments for an account since the given timestamp
func (c *Client) FetchFundingPayments(
	ctx context.Context,
	account *models.ExchangeAccount,
	since time.Time,
) ([]*models.FundingPaymentInput, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	accountUUID, err := uuid.Parse(account.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid account ID: %w", err)
	}

	authToken, err := c.getAuthToken(account)
	if err != nil {
		return nil, err
	}

	accountIndex := account.AccountIdentifier
	if accountIndex == "" {
		return nil, fmt.Errorf("account identifier (account_index) is required")
	}

	// Ensure market cache is loaded
	if err := c.loadMarketCache(ctx, authToken); err != nil {
		return nil, fmt.Errorf("failed to load market info: %w", err)
	}

	var allFunding []*models.FundingPaymentInput
	cursor := ""
	pageCount := 0

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Add delay between pagination requests to avoid rate limits
		if pageCount > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(paginationDelay):
			}
		}

		var fundings []lighterPositionFunding
		var nextCursor string
		var err error

		// Retry logic for rate limits
		for retry := 0; retry <= maxRateLimitRetries; retry++ {
			fundings, nextCursor, err = c.fetchPositionFunding(ctx, accountIndex, authToken, cursor, since)
			if err == nil {
				break
			}

			// Check if it's a rate limit error
			var rateLimitErr *iface.RateLimitError
			if errors.As(err, &rateLimitErr) && retry < maxRateLimitRetries {
				waitTime := rateLimitErr.RetryAfter
				if waitTime < time.Second {
					waitTime = 30 * time.Second
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(waitTime):
				}
				continue
			}
			return nil, err
		}

		pageCount++

		for _, funding := range fundings {
			// Timestamp is in Unix seconds
			ts := time.Unix(funding.Timestamp, 0).UTC()
			if !since.IsZero() && ts.Before(since) {
				continue
			}

			payment, err := c.fundingToPayment(funding, accountUUID)
			if err != nil {
				continue // Skip fundings that can't be converted
			}

			allFunding = append(allFunding, payment)
		}

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Sort by timestamp (oldest first)
	sort.Slice(allFunding, func(i, j int) bool {
		return allFunding[i].Timestamp.Before(allFunding[j].Timestamp)
	})

	return allFunding, nil
}

// getAuthToken extracts the auth token from account metadata
func (c *Client) getAuthToken(account *models.ExchangeAccount) (string, error) {
	if account.AccountTypeMetadata == nil {
		return "", fmt.Errorf("account metadata is required for Lighter authentication")
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(account.AccountTypeMetadata, &metadata); err != nil {
		return "", fmt.Errorf("failed to parse account metadata: %w", err)
	}

	token, ok := metadata["auth_token"].(string)
	if !ok || token == "" {
		return "", fmt.Errorf("auth_token not found in account metadata")
	}

	return token, nil
}

// addAuth adds the Authorization header to a request
func (c *Client) addAuth(req *http.Request, authToken string) {
	req.Header.Set("Authorization", authToken)
}

// fetchTrades fetches individual trades/fills from the API
func (c *Client) fetchTrades(
	ctx context.Context,
	accountIndex string,
	authToken string,
	cursor string,
) ([]lighterTrade, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/trades", c.baseURL)

	params := url.Values{}
	params.Set("account_index", accountIndex)
	params.Set("sort_by", "timestamp")
	params.Set("limit", "100")
	params.Set("aggregate", "false") // Get individual fills, not aggregated

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req, authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.handleResponseError(resp); err != nil {
		return nil, "", err
	}

	var result lighterTradesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Trades, result.NextCursor, nil
}

// fetchInactiveOrders fetches inactive orders (including filled) from the API
// Note: This returns orders with aggregate fills, not individual trades
func (c *Client) fetchInactiveOrders(
	ctx context.Context,
	accountIndex string,
	authToken string,
	cursor string,
	since time.Time,
) ([]lighterOrder, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/accountInactiveOrders", c.baseURL)

	params := url.Values{}
	params.Set("account_index", accountIndex)
	params.Set("limit", "100")

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	// Note: between_timestamps parameter format is unclear from API,
	// so we filter client-side instead

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req, authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.handleResponseError(resp); err != nil {
		return nil, "", err
	}

	var result lighterOrdersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Orders, result.NextCursor, nil
}

// fetchPositionFunding fetches funding payments from the API
func (c *Client) fetchPositionFunding(
	ctx context.Context,
	accountIndex string,
	authToken string,
	cursor string,
	since time.Time,
) ([]lighterPositionFunding, string, error) {
	endpoint := fmt.Sprintf("%s/api/v1/positionFunding", c.baseURL)

	params := url.Values{}
	params.Set("account_index", accountIndex)
	params.Set("limit", "100")

	if cursor != "" {
		params.Set("cursor", cursor)
	}

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	c.addAuth(req, authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := c.handleResponseError(resp); err != nil {
		return nil, "", err
	}

	var result lighterFundingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.PositionFundings, result.NextCursor, nil
}

// loadMarketCache loads market information from the API
func (c *Client) loadMarketCache(ctx context.Context, authToken string) error {
	c.marketCache.mu.RLock()
	if c.marketCache.loaded {
		c.marketCache.mu.RUnlock()
		return nil
	}
	c.marketCache.mu.RUnlock()

	c.marketCache.mu.Lock()
	defer c.marketCache.mu.Unlock()

	// Double-check after acquiring write lock
	if c.marketCache.loaded {
		return nil
	}

	endpoint := fmt.Sprintf("%s/api/v1/orderBookDetails", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// orderBookDetails is a public endpoint, but add auth if available
	if authToken != "" {
		c.addAuth(req, authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch markets: status %d", resp.StatusCode)
	}

	var result lighterMarketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	// Add perp markets
	for _, m := range result.OrderBookDetails {
		// Symbol is the base asset (e.g., "ASTER", "BTC", "ETH")
		// All perps on Lighter are quoted in USDC
		c.marketCache.markets[m.MarketID] = marketInfo{
			symbol:     m.Symbol,
			baseAsset:  m.Symbol,
			quoteAsset: "USDC",
			marketType: "perp",
		}
	}

	// Add spot markets
	for _, m := range result.SpotOrderBookDetails {
		// Spot market symbols are "BASE/QUOTE" (e.g., "LIT/USDC")
		baseAsset := m.Symbol
		quoteAsset := "USDC"
		if parts := strings.Split(m.Symbol, "/"); len(parts) == 2 {
			baseAsset = parts[0]
			quoteAsset = parts[1]
		}
		c.marketCache.markets[m.MarketID] = marketInfo{
			symbol:     m.Symbol,
			baseAsset:  baseAsset,
			quoteAsset: quoteAsset,
			marketType: "spot",
		}
	}

	c.marketCache.loaded = true
	return nil
}

// getMarketInfo returns cached market information
func (c *Client) getMarketInfo(marketIndex int) (marketInfo, bool) {
	c.marketCache.mu.RLock()
	defer c.marketCache.mu.RUnlock()

	info, ok := c.marketCache.markets[marketIndex]
	return info, ok
}

// tradeToTradeInput converts a Lighter trade (from /api/v1/trades) to a TradeInput
func (c *Client) tradeToTradeInput(trade lighterTrade, accountIndex string, accountUUID uuid.UUID) (*models.TradeInput, error) {
	market, ok := c.getMarketInfo(trade.MarketID)
	if !ok {
		// Fallback: use generic naming and default to perp
		market = marketInfo{
			baseAsset:  fmt.Sprintf("MARKET%d", trade.MarketID),
			quoteAsset: "USDC",
			marketType: "perp",
		}
	}

	// Determine side based on whether this account was the buyer or seller
	// ask = sell side, bid = buy side
	// If ask_account_id matches our account, we were selling
	// If bid_account_id matches our account, we were buying
	accountIndexInt, _ := strconv.ParseInt(accountIndex, 10, 64)
	side := "buy"
	orderID := fmt.Sprintf("%d", trade.BidID)
	if trade.AskAccountID == accountIndexInt {
		side = "sell"
		orderID = fmt.Sprintf("%d", trade.AskID)
	}

	// Timestamp is in Unix milliseconds
	ts := time.Unix(0, trade.Timestamp*int64(time.Millisecond)).UTC()

	// Calculate fee (taker_fee is in basis points, e.g., 200 = 0.02%)
	// Fee is applied to the USD amount
	fee := "0"
	if trade.TakerFee > 0 {
		// Convert taker_fee basis points to decimal fee amount
		// taker_fee of 200 means 0.02% fee
		// fee = usd_amount * taker_fee / 1000000
		if usdAmt, ok := new(big.Float).SetString(trade.USDAmount); ok {
			feePct := new(big.Float).SetInt64(trade.TakerFee)
			feeAmt := new(big.Float).Mul(usdAmt, feePct)
			feeAmt = feeAmt.Quo(feeAmt, big.NewFloat(1000000))
			fee = strings.TrimRight(strings.TrimRight(feeAmt.Text('f', 8), "0"), ".")
		}
	}

	return &models.TradeInput{
		ExchangeAccountID: accountUUID,
		BaseAsset:         market.baseAsset,
		QuoteAsset:        market.quoteAsset,
		Side:              side,
		Price:             trade.Price,
		Quantity:          trade.Size,
		Timestamp:         ts,
		Fee:               fee,
		OrderID:           orderID,
		TradeID:           fmt.Sprintf("%d", trade.TradeID),
		MarketType:        market.marketType,
	}, nil
}

// orderToTrade converts a Lighter order to a TradeInput
// Note: This is kept for backwards compatibility but fetchTrades now uses tradeToTradeInput
func (c *Client) orderToTrade(order lighterOrder, accountUUID uuid.UUID) (*models.TradeInput, error) {
	market, ok := c.getMarketInfo(order.MarketIndex)
	if !ok {
		// Fallback: use generic naming and default to perp
		market = marketInfo{
			baseAsset:  fmt.Sprintf("MARKET%d", order.MarketIndex),
			quoteAsset: "USDC",
			marketType: "perp",
		}
	}

	// Use the price field directly if available, otherwise calculate
	price := order.Price
	if price == "" || price == "0" {
		price = calculatePrice(order.FilledQuoteAmount, order.FilledBaseAmount)
	}

	// Determine side: is_ask = true means sell, is_ask = false means buy
	side := "buy"
	if order.IsAsk {
		side = "sell"
	}

	// Timestamp is in Unix seconds
	ts := time.Unix(order.Timestamp, 0).UTC()

	return &models.TradeInput{
		ExchangeAccountID: accountUUID,
		BaseAsset:         market.baseAsset,
		QuoteAsset:        market.quoteAsset,
		Side:              side,
		Price:             price,
		Quantity:          order.FilledBaseAmount,
		Timestamp:         ts,
		Fee:               "0", // Lighter doesn't return fee in order response
		OrderID:           order.OrderID,
		TradeID:           order.OrderID, // Order ID serves as trade ID for filled orders
		MarketType:        market.marketType,
	}, nil
}

// fundingToPayment converts a Lighter funding to a FundingPaymentInput
func (c *Client) fundingToPayment(funding lighterPositionFunding, accountUUID uuid.UUID) (*models.FundingPaymentInput, error) {
	market, ok := c.getMarketInfo(funding.MarketID)
	if !ok {
		// Fallback
		market = marketInfo{
			baseAsset:  fmt.Sprintf("MARKET%d", funding.MarketID),
			quoteAsset: "USDC",
		}
	}

	// Timestamp is in Unix seconds
	ts := time.Unix(funding.Timestamp, 0).UTC()

	// Create unique payment ID from funding_id and market_id
	paymentID := fmt.Sprintf("%d_%d", funding.FundingID, funding.MarketID)

	return &models.FundingPaymentInput{
		ExchangeAccountID: accountUUID,
		BaseAsset:         market.baseAsset,
		QuoteAsset:        market.quoteAsset,
		Amount:            funding.Change,
		Timestamp:         ts,
		PaymentID:         paymentID,
	}, nil
}

// handleResponseError checks response status and returns appropriate errors
func (c *Client) handleResponseError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		return &iface.RateLimitError{
			Exchange:   "lighter",
			Message:    "rate limit exceeded",
			RetryAfter: retryAfter,
		}
	case http.StatusUnauthorized:
		return fmt.Errorf("authentication failed: invalid or expired auth token")
	case http.StatusForbidden:
		return fmt.Errorf("access denied: insufficient permissions")
	case http.StatusNotFound:
		return fmt.Errorf("account not found")
	default:
		return fmt.Errorf("request failed with status %d", resp.StatusCode)
	}
}

// parseRetryAfter parses the Retry-After header value
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 60 * time.Second // Default to 60 seconds
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		return time.Until(t)
	}

	return 60 * time.Second
}

// calculatePrice divides quote amount by base amount to get price
func calculatePrice(quoteAmount, baseAmount string) string {
	if baseAmount == "" || baseAmount == "0" {
		return "0"
	}

	quote := new(big.Float)
	if _, ok := quote.SetString(quoteAmount); !ok {
		return "0"
	}

	base := new(big.Float)
	if _, ok := base.SetString(baseAmount); !ok {
		return "0"
	}

	if base.Sign() == 0 {
		return "0"
	}

	price := new(big.Float).Quo(quote, base)

	// Format with enough precision
	return strings.TrimRight(strings.TrimRight(price.Text('f', 18), "0"), ".")
}
